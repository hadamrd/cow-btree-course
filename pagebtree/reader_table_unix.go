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
	readerTableVersion    = uint64(1)
	readerTableSlotCount  = 64
	readerTableHeaderSize = 32
	readerSlotSize        = 32
	readerTableSize       = readerTableHeaderSize + readerTableSlotCount*readerSlotSize
)

var readerTokenCounter uint64

type readerTable struct {
	file     *os.File
	slot     int
	token    uint64
	revision uint64
}

type readerSlot struct {
	active   bool
	pid      int
	revision uint64
	token    uint64
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
			if current.active && pidAlive(current.pid) {
				continue
			}
			if current.active {
				if err := r.writeSlotActiveLocked(slot, false); err != nil {
					return err
				}
			}
			if err := r.writeSlotLocked(slot, readerSlot{
				active:   true,
				pid:      os.Getpid(),
				revision: revision,
				token:    token,
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
		if current.active && current.token == token && current.pid == os.Getpid() {
			return r.writeSlotActiveLocked(slot, false)
		}
		return nil
	})
}

func (r *readerTable) oldest() (uint64, bool, error) {
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
			if !pidAlive(current.pid) {
				if err := r.writeSlotActiveLocked(slot, false); err != nil {
					return err
				}
				continue
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

func (r *readerTable) stats() (MmapReaderStats, error) {
	stats, _, err := r.scan(false)
	return stats, err
}

func (r *readerTable) cleanStale() (int, error) {
	_, cleared, err := r.scan(true)
	return cleared, err
}

func (r *readerTable) close() error {
	if r == nil {
		return nil
	}
	return errors.Join(r.release(), r.file.Close())
}

func (r *readerTable) scan(cleanStale bool) (MmapReaderStats, int, error) {
	stats := MmapReaderStats{Slots: readerTableSlotCount}
	cleared := 0
	if r == nil {
		return MmapReaderStats{}, 0, nil
	}
	err := r.withLock(func() error {
		for slot := 0; slot < readerTableSlotCount; slot++ {
			current, err := r.readSlotLocked(slot)
			if err != nil {
				return err
			}
			if !current.active {
				continue
			}
			if !pidAlive(current.pid) {
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
			if !stats.HasOldestRevision || current.revision < stats.OldestRevision {
				stats.OldestRevision = current.revision
				stats.HasOldestRevision = true
			}
		}
		return nil
	})
	return stats, cleared, err
}

// MmapReaderStats reports the current mmap reader-table slots for this tree.
func (t *Tree) MmapReaderStats() (MmapReaderStats, error) {
	if t == nil || t.closed || t.arena == nil || t.arena.readerTable == nil {
		return MmapReaderStats{}, nil
	}
	return t.arena.readerTable.stats()
}

// CleanStaleMmapReaders clears reader-table slots owned by dead processes.
func (t *Tree) CleanStaleMmapReaders() (int, error) {
	if t == nil || t.closed || t.arena == nil || t.arena.readerTable == nil {
		return 0, nil
	}
	return t.arena.readerTable.cleanStale()
}

func (r *readerTable) ensureFileLocked() error {
	info, err := r.file.Stat()
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return r.initializeFileLocked()
	}
	if info.Size() != readerTableSize {
		return fmt.Errorf("%w: reader table size %d, want %d", ErrReaderTable, info.Size(), readerTableSize)
	}

	header := make([]byte, readerTableHeaderSize)
	if _, err := r.file.ReadAt(header, 0); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if string(header[:8]) == readerTableMagic &&
		binary.LittleEndian.Uint64(header[8:16]) == readerTableVersion &&
		binary.LittleEndian.Uint64(header[16:24]) == readerTableSlotCount {
		return nil
	}
	return fmt.Errorf("%w: reader table header mismatch", ErrReaderTable)
}

func (r *readerTable) initializeFileLocked() error {
	if err := r.file.Truncate(readerTableSize); err != nil {
		return err
	}
	zero := make([]byte, readerTableSize)
	copy(zero[:8], readerTableMagic)
	binary.LittleEndian.PutUint64(zero[8:16], readerTableVersion)
	binary.LittleEndian.PutUint64(zero[16:24], readerTableSlotCount)
	_, err := r.file.WriteAt(zero, 0)
	return err
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
	buf := make([]byte, readerSlotSize)
	_, err := r.file.ReadAt(buf, int64(readerTableHeaderSize+slot*readerSlotSize))
	if err != nil && !errors.Is(err, io.EOF) {
		return readerSlot{}, err
	}
	return readerSlot{
		active:   binary.LittleEndian.Uint64(buf[0:8]) != 0,
		pid:      int(binary.LittleEndian.Uint64(buf[8:16])),
		revision: binary.LittleEndian.Uint64(buf[16:24]),
		token:    binary.LittleEndian.Uint64(buf[24:32]),
	}, nil
}

func (r *readerTable) writeSlotLocked(slot int, value readerSlot) error {
	buf := make([]byte, readerSlotSize)
	if value.active {
		binary.LittleEndian.PutUint64(buf[0:8], 1)
	}
	binary.LittleEndian.PutUint64(buf[8:16], uint64(value.pid))
	binary.LittleEndian.PutUint64(buf[16:24], value.revision)
	binary.LittleEndian.PutUint64(buf[24:32], value.token)
	_, err := r.file.WriteAt(buf, int64(readerTableHeaderSize+slot*readerSlotSize))
	return err
}

func (r *readerTable) writeSlotActiveLocked(slot int, active bool) error {
	var buf [8]byte
	if active {
		binary.LittleEndian.PutUint64(buf[:], 1)
	}
	_, err := r.file.WriteAt(buf[:], int64(readerTableHeaderSize+slot*readerSlotSize))
	return err
}

func nextReaderToken() uint64 {
	return uint64(os.Getpid())<<32 ^
		uint64(time.Now().UnixNano()) ^
		atomic.AddUint64(&readerTokenCounter, 1)
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := unix.Kill(pid, 0)
	return err == nil || errors.Is(err, unix.EPERM)
}
