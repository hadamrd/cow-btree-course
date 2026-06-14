//go:build unix

package pagebtree

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

const (
	readerTableMagic      = "CBTRDR1\x00"
	readerTableVersion    = uint64(3)
	readerTableV2Version  = uint64(2)
	readerTableV1Version  = uint64(1)
	readerTableSlotCount  = 64
	readerTableHeaderSize = 32
	readerSlotV1Size      = 32
	readerSlotV2Size      = 40
	readerSlotSize        = 48
	readerTableV1Size     = readerTableHeaderSize + readerTableSlotCount*readerSlotV1Size
	readerTableV2Size     = readerTableHeaderSize + readerTableSlotCount*readerSlotV2Size
	readerTableSize       = readerTableHeaderSize + readerTableSlotCount*readerSlotSize
)

var (
	readerTokenCounter uint64
	pidAlive           = pidAliveUnix
	processStartToken  = processStartTokenUnix
	bootIDToken        = bootIDTokenUnix
)

type readerTable struct {
	file      *os.File
	slot      int
	token     uint64
	revision  uint64
	version   uint64
	slotSize  int
	tableSize int
}

type readerSlot struct {
	active       bool
	pid          int
	processStart uint64
	bootID       uint64
	revision     uint64
	token        uint64
}

func openReaderTable(dbPath string) (*readerTable, error) {
	file, err := os.OpenFile(dbPath+".readers", os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	table := &readerTable{file: file, slot: -1}
	if err := table.withLock(table.ensureFileLocked); err != nil {
		file.Close()
		return nil, err
	}
	return table, nil
}

func openWriterLock(dbPath string) (*os.File, error) {
	file, err := os.OpenFile(dbPath+".writer", os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	if err := lockFile(file, true); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func closeWriterLock(file *os.File) error {
	if file == nil {
		return nil
	}
	return errors.Join(unlockFile(file), file.Close())
}

func (r *readerTable) claim(revision uint64) error {
	if r == nil {
		return nil
	}
	token := nextReaderToken()
	err := r.withLock(func() error {
		for slot := 0; slot < readerTableSlotCount; slot++ {
			current, err := r.readSlotLocked(slot)
			if err != nil {
				return err
			}
			if current.active && readerSlotAlive(current) {
				continue
			}
			if current.active {
				if err := r.writeSlotActiveLocked(slot, false); err != nil {
					return err
				}
			}
			if err := r.writeSlotLocked(slot, readerSlot{
				active:       true,
				pid:          os.Getpid(),
				processStart: processStartToken(os.Getpid()),
				bootID:       bootIDToken(),
				revision:     revision,
				token:        token,
			}); err != nil {
				return err
			}
			r.slot = slot
			r.token = token
			r.revision = revision
			return nil
		}
		return ErrActiveReaders
	})
	return err
}

func (r *readerTable) release() error {
	if r == nil || r.slot < 0 {
		return nil
	}
	slot := r.slot
	token := r.token
	r.slot = -1
	return r.withLock(func() error {
		current, err := r.readSlotLocked(slot)
		if err != nil {
			return err
		}
		if current.active && current.token == token && r.ownsSlot(current) {
			return r.writeSlotActiveLocked(slot, false)
		}
		return nil
	})
}

func (r *readerTable) updateRevision(revision uint64) error {
	if r == nil || r.slot < 0 {
		return nil
	}
	slot := r.slot
	token := r.token
	return r.withLock(func() error {
		current, err := r.readSlotLocked(slot)
		if err != nil {
			return err
		}
		if current.active && current.token == token && r.ownsSlot(current) {
			current.revision = revision
			return r.writeSlotLocked(slot, current)
		}
		return ErrActiveReaders
	})
}

func (r *readerTable) oldest(maxRevision uint64) (uint64, bool, error) {
	if r == nil {
		return 0, false, nil
	}
	var oldest uint64
	found := false
	err := r.withLock(func() error {
		for slot := 0; slot < readerTableSlotCount; slot++ {
			current, err := r.readSlotLocked(slot)
			if err != nil {
				return err
			}
			if !current.active {
				continue
			}
			if !readerSlotAlive(current) {
				if err := r.writeSlotActiveLocked(slot, false); err != nil {
					return err
				}
				continue
			}
			if err := validateReaderSlotRevision(slot, current, maxRevision); err != nil {
				return err
			}
			if !found || current.revision < oldest {
				oldest = current.revision
				found = true
			}
		}
		return nil
	})
	return oldest, found, err
}

func (r *readerTable) stats(maxRevision uint64) (MmapReaderStats, error) {
	stats, _, err := r.scan(maxRevision, false)
	return stats, err
}

func (r *readerTable) cleanStale(maxRevision uint64) (int, error) {
	_, cleared, err := r.scan(maxRevision, true)
	return cleared, err
}

func (r *readerTable) validate(maxRevision uint64) error {
	_, _, err := r.scan(maxRevision, false)
	return err
}

func (r *readerTable) close() error {
	if r == nil {
		return nil
	}
	return errors.Join(r.release(), r.file.Close())
}

func (r *readerTable) scan(maxRevision uint64, cleanStale bool) (MmapReaderStats, int, error) {
	if r == nil {
		return MmapReaderStats{}, 0, nil
	}
	stats := MmapReaderStats{
		FormatVersion:              r.version,
		ProcessStartTokenSupported: processStartToken(os.Getpid()) != 0,
		BootIDTokenSupported:       bootIDToken() != 0,
		Slots:                      readerTableSlotCount,
	}
	cleared := 0
	err := r.withLock(func() error {
		for slot := 0; slot < readerTableSlotCount; slot++ {
			current, err := r.readSlotLocked(slot)
			if err != nil {
				return err
			}
			if !current.active {
				continue
			}
			if current.processStart != 0 {
				stats.ProcessStartSlots++
			}
			if current.bootID != 0 {
				stats.BootIDSlots++
			}
			if !readerSlotAlive(current) {
				stats.StaleSlots++
				if cleanStale {
					if err := r.writeSlotActiveLocked(slot, false); err != nil {
						return err
					}
					cleared++
				}
				continue
			}
			stats.ActiveSlots++
			if err := validateReaderSlotRevision(slot, current, maxRevision); err != nil {
				return err
			}
			if !stats.HasOldestRevision || current.revision < stats.OldestRevision {
				stats.OldestRevision = current.revision
				stats.HasOldestRevision = true
			}
		}
		return nil
	})
	return stats, cleared, err
}

func validateReaderSlotRevision(slot int, current readerSlot, maxRevision uint64) error {
	if current.token == 0 {
		return fmt.Errorf("%w: reader slot %d has zero token", ErrReaderTable, slot)
	}
	if current.revision > maxRevision {
		return fmt.Errorf("%w: reader slot %d revision %d beyond tree revision %d", ErrReaderTable, slot, current.revision, maxRevision)
	}
	return nil
}

// MmapReaderStats reports the current mmap reader-table slots for this tree.
func (t *Tree) MmapReaderStats() (MmapReaderStats, error) {
	if t == nil || t.closed || t.arena == nil || t.arena.readerTable == nil {
		return MmapReaderStats{}, nil
	}
	return t.arena.readerTable.stats(t.revision)
}

// CleanStaleMmapReaders clears reader-table slots owned by dead or reused owners.
func (t *Tree) CleanStaleMmapReaders() (int, error) {
	if t == nil || t.closed || t.arena == nil || t.arena.readerTable == nil {
		return 0, nil
	}
	cleared, err := t.arena.readerTable.cleanStale(t.revision)
	if err != nil {
		return cleared, err
	}
	t.emitMmapTraceReaderCleanup(cleared)
	return cleared, nil
}

func (r *readerTable) ensureFileLocked() error {
	info, err := r.file.Stat()
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return r.initializeFileLocked()
	}
	if info.Size() < readerTableHeaderSize {
		return fmt.Errorf("%w: reader table size %d, want at least %d", ErrReaderTable, info.Size(), readerTableHeaderSize)
	}

	header := make([]byte, readerTableHeaderSize)
	if _, err := r.file.ReadAt(header, 0); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if string(header[:8]) != readerTableMagic || binary.LittleEndian.Uint64(header[16:24]) != readerTableSlotCount {
		return fmt.Errorf("%w: reader table header mismatch", ErrReaderTable)
	}
	version := binary.LittleEndian.Uint64(header[8:16])
	if err := r.setFormat(version); err != nil {
		return err
	}
	if info.Size() != int64(r.tableSize) {
		return fmt.Errorf("%w: reader table size %d, want %d", ErrReaderTable, info.Size(), r.tableSize)
	}
	return nil
}

func (r *readerTable) initializeFileLocked() error {
	if err := r.setFormat(readerTableVersion); err != nil {
		return err
	}
	if err := r.file.Truncate(int64(r.tableSize)); err != nil {
		return err
	}
	zero := make([]byte, r.tableSize)
	copy(zero[:8], readerTableMagic)
	binary.LittleEndian.PutUint64(zero[8:16], readerTableVersion)
	binary.LittleEndian.PutUint64(zero[16:24], readerTableSlotCount)
	_, err := r.file.WriteAt(zero, 0)
	return err
}

func (r *readerTable) setFormat(version uint64) error {
	switch version {
	case readerTableV1Version:
		r.version = version
		r.slotSize = readerSlotV1Size
		r.tableSize = readerTableV1Size
	case readerTableV2Version:
		r.version = version
		r.slotSize = readerSlotV2Size
		r.tableSize = readerTableV2Size
	case readerTableVersion:
		r.version = version
		r.slotSize = readerSlotSize
		r.tableSize = readerTableSize
	default:
		return fmt.Errorf("%w: reader table version %d", ErrReaderTable, version)
	}
	return nil
}

func (r *readerTable) withLock(fn func() error) error {
	if err := unix.Flock(int(r.file.Fd()), unix.LOCK_EX); err != nil {
		return err
	}
	err := fn()
	unlockErr := unix.Flock(int(r.file.Fd()), unix.LOCK_UN)
	return errors.Join(err, unlockErr)
}

func (r *readerTable) readSlotLocked(slot int) (readerSlot, error) {
	buf := make([]byte, r.slotSize)
	_, err := r.file.ReadAt(buf, int64(readerTableHeaderSize+slot*r.slotSize))
	if err != nil && !errors.Is(err, io.EOF) {
		return readerSlot{}, err
	}
	current := readerSlot{
		active:   binary.LittleEndian.Uint64(buf[0:8]) != 0,
		pid:      int(binary.LittleEndian.Uint64(buf[8:16])),
		revision: binary.LittleEndian.Uint64(buf[16:24]),
		token:    binary.LittleEndian.Uint64(buf[24:32]),
	}
	if r.slotSize >= readerSlotV2Size {
		current.processStart = binary.LittleEndian.Uint64(buf[32:40])
	}
	if r.slotSize >= readerSlotSize {
		current.bootID = binary.LittleEndian.Uint64(buf[40:48])
	}
	return current, nil
}

func (r *readerTable) writeSlotLocked(slot int, value readerSlot) error {
	buf := make([]byte, r.slotSize)
	if value.active {
		binary.LittleEndian.PutUint64(buf[0:8], 1)
	}
	binary.LittleEndian.PutUint64(buf[8:16], uint64(value.pid))
	binary.LittleEndian.PutUint64(buf[16:24], value.revision)
	binary.LittleEndian.PutUint64(buf[24:32], value.token)
	if r.slotSize >= readerSlotV2Size {
		binary.LittleEndian.PutUint64(buf[32:40], value.processStart)
	}
	if r.slotSize >= readerSlotSize {
		binary.LittleEndian.PutUint64(buf[40:48], value.bootID)
	}
	_, err := r.file.WriteAt(buf, int64(readerTableHeaderSize+slot*r.slotSize))
	return err
}

func (r *readerTable) writeSlotActiveLocked(slot int, active bool) error {
	var buf [8]byte
	if active {
		binary.LittleEndian.PutUint64(buf[:], 1)
	}
	_, err := r.file.WriteAt(buf[:], int64(readerTableHeaderSize+slot*r.slotSize))
	return err
}

func nextReaderToken() uint64 {
	token := uint64(os.Getpid())<<32 ^
		uint64(time.Now().UnixNano()) ^
		atomic.AddUint64(&readerTokenCounter, 1)
	if token == 0 {
		return 1
	}
	return token
}

func (r *readerTable) ownsSlot(current readerSlot) bool {
	if current.pid != os.Getpid() {
		return false
	}
	if current.processStart == 0 {
		return bootIDMatches(current)
	}
	start := processStartToken(current.pid)
	return (start == 0 || start == current.processStart) && bootIDMatches(current)
}

func readerSlotAlive(current readerSlot) bool {
	if !pidAlive(current.pid) {
		return false
	}
	if current.processStart == 0 {
		return bootIDMatches(current)
	}
	start := processStartToken(current.pid)
	return (start == 0 || start == current.processStart) && bootIDMatches(current)
}

func bootIDMatches(current readerSlot) bool {
	if current.bootID == 0 {
		return true
	}
	boot := bootIDToken()
	return boot == 0 || boot == current.bootID
}

func pidAliveUnix(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := unix.Kill(pid, 0)
	return err == nil || errors.Is(err, unix.EPERM)
}
