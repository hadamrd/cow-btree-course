package pagebtree

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
)

// PageSize is the teaching target used in the docs. This package keeps page
// contents in a fixed byte array so the code demonstrates the usual slotted
// page shape: header and slots grow right, cells grow left.
const PageSize = 4096

type PageID uint64

var (
	ErrPageChecksum  = errors.New("page checksum mismatch")
	ErrPageLayout    = errors.New("page layout invalid")
	ErrTreeInvariant = errors.New("tree invariant invalid")
)

const (
	pageHeaderSize = 20
	slotSize       = 8

	headerFlagsOffset     = 0
	headerSlotCountOffset = 2
	headerFreeUpperOffset = 4
	headerLeftmostOffset  = 8
	headerChecksumOffset  = 16

	flagLeaf     = uint16(0x01)
	flagBranch   = uint16(0x02)
	flagOverflow = uint16(0x04)
)

type slot struct {
	offset   uint16
	keyLen   uint16
	valueLen uint16
	flags    uint16
}

type page struct {
	id   PageID
	data []byte
}

func newLeaf(id PageID, key string, value []byte) *page {
	p := newPage(id, flagLeaf)
	mustWriteLeafEntries(p, []leafEntry{{key: key, value: cloneBytes(value)}})
	return p
}

func newPage(id PageID, flags uint16) *page {
	p := &page{id: id, data: make([]byte, PageSize)}
	p.setFlags(flags)
	p.setSlotCount(0)
	p.setFreeUpper(PageSize)
	p.updateChecksum()
	return p
}

func (p *page) clone(id PageID) *page {
	out := newPage(id, p.flags())
	copy(out.data[:], p.data[:])
	return out
}

func (p *page) full(degree int) bool {
	return int(p.slotCount()) == maxKeys(degree)
}

func (p *page) flags() uint16 {
	return binary.LittleEndian.Uint16(p.data[headerFlagsOffset:])
}

func (p *page) setFlags(flags uint16) {
	binary.LittleEndian.PutUint16(p.data[headerFlagsOffset:], flags)
}

func (p *page) isLeaf() bool {
	return p.flags()&flagLeaf != 0
}

func (p *page) isBranch() bool {
	return p.flags()&flagBranch != 0
}

func (p *page) isOverflow() bool {
	return p.flags()&flagOverflow != 0
}

func (p *page) slotCount() uint16 {
	return binary.LittleEndian.Uint16(p.data[headerSlotCountOffset:])
}

func (p *page) setSlotCount(count uint16) {
	binary.LittleEndian.PutUint16(p.data[headerSlotCountOffset:], count)
}

func (p *page) freeUpper() uint16 {
	return binary.LittleEndian.Uint16(p.data[headerFreeUpperOffset:])
}

func (p *page) setFreeUpper(offset int) {
	binary.LittleEndian.PutUint16(p.data[headerFreeUpperOffset:], uint16(offset))
}

func (p *page) leftmostChild() PageID {
	return decodePageID(p.data[headerLeftmostOffset : headerLeftmostOffset+8])
}

func (p *page) setLeftmostChild(id PageID) {
	encodePageID(p.data[headerLeftmostOffset:headerLeftmostOffset+8], id)
}

func (p *page) nextLeaf() PageID {
	return p.leftmostChild()
}

func (p *page) setNextLeaf(id PageID) {
	p.setLeftmostChild(id)
	p.updateChecksum()
}

func (p *page) checksum() uint32 {
	return binary.LittleEndian.Uint32(p.data[headerChecksumOffset:])
}

func (p *page) updateChecksum() {
	binary.LittleEndian.PutUint32(p.data[headerChecksumOffset:], p.computeChecksum())
}

func (p *page) validChecksum() bool {
	return p.checksum() == p.computeChecksum()
}

func (p *page) computeChecksum() uint32 {
	checksum := crc32.NewIEEE()
	_, _ = checksum.Write(p.data[:headerChecksumOffset])
	_, _ = checksum.Write(p.data[headerChecksumOffset+4 : PageSize])
	return checksum.Sum32()
}

func slotBase(index int) int {
	return pageHeaderSize + index*slotSize
}

func (p *page) validateLayout() error {
	switch p.flags() {
	case flagLeaf:
		return p.validateSlottedLayout()
	case flagBranch:
		if p.leftmostChild() == 0 {
			return fmt.Errorf("%w: branch page %d has zero leftmost child", ErrPageLayout, p.id)
		}
		return p.validateSlottedLayout()
	case flagOverflow:
		return p.validateOverflowLayout()
	default:
		return fmt.Errorf("%w: page %d has invalid flags %x", ErrPageLayout, p.id, p.flags())
	}
}

func (p *page) validateSlottedLayout() error {
	slotCount := int(p.slotCount())
	slotEnd := pageHeaderSize + slotCount*slotSize
	if slotEnd > PageSize {
		return fmt.Errorf("%w: page %d slot directory ends at %d", ErrPageLayout, p.id, slotEnd)
	}
	freeUpper := int(p.freeUpper())
	if freeUpper < slotEnd || freeUpper > PageSize {
		return fmt.Errorf("%w: page %d freeUpper %d outside [%d,%d]", ErrPageLayout, p.id, freeUpper, slotEnd, PageSize)
	}

	ranges := make([]cellRange, 0, slotCount)
	var previousKey string
	for i := 0; i < slotCount; i++ {
		slot := p.readSlot(i)
		cellStart := int(slot.offset)
		keyEnd := cellStart + int(slot.keyLen)
		cellEnd := keyEnd + int(slot.valueLen)
		if cellStart < freeUpper || cellStart > PageSize || keyEnd > PageSize || cellEnd > PageSize {
			return fmt.Errorf("%w: page %d slot %d cell range [%d,%d) outside cell area [%d,%d)", ErrPageLayout, p.id, i, cellStart, cellEnd, freeUpper, PageSize)
		}
		if p.isBranch() {
			if slot.valueLen != 8 {
				return fmt.Errorf("%w: branch page %d slot %d child value length %d", ErrPageLayout, p.id, i, slot.valueLen)
			}
			if p.readCellPageID(i) == 0 {
				return fmt.Errorf("%w: branch page %d slot %d has zero child", ErrPageLayout, p.id, i)
			}
			if slot.flags != 0 {
				return fmt.Errorf("%w: branch page %d slot %d has flags %x", ErrPageLayout, p.id, i, slot.flags)
			}
		}
		if p.isLeaf() && slot.flags&^slotFlagOverflow != 0 {
			return fmt.Errorf("%w: leaf page %d slot %d has flags %x", ErrPageLayout, p.id, i, slot.flags)
		}
		key := p.readCellKey(i)
		if i > 0 && previousKey >= key {
			return fmt.Errorf("%w: page %d slot keys out of order at %d", ErrPageLayout, p.id, i)
		}
		previousKey = key
		ranges = append(ranges, cellRange{start: cellStart, end: cellEnd})
	}
	for i := 0; i < len(ranges); i++ {
		for j := i + 1; j < len(ranges); j++ {
			if ranges[i].overlaps(ranges[j]) {
				return fmt.Errorf("%w: page %d slots %d and %d overlap", ErrPageLayout, p.id, i, j)
			}
		}
	}
	return nil
}

func (p *page) validateOverflowLayout() error {
	if p.overflowPayloadLen() > overflowPayloadSize {
		return fmt.Errorf("%w: overflow page %d payload length %d exceeds capacity", ErrPageLayout, p.id, p.overflowPayloadLen())
	}
	return nil
}

type cellRange struct {
	start int
	end   int
}

func (r cellRange) overlaps(other cellRange) bool {
	return r.start < other.end && other.start < r.end
}

func (p *page) readSlot(index int) slot {
	base := slotBase(index)
	return slot{
		offset:   binary.LittleEndian.Uint16(p.data[base:]),
		keyLen:   binary.LittleEndian.Uint16(p.data[base+2:]),
		valueLen: binary.LittleEndian.Uint16(p.data[base+4:]),
		flags:    binary.LittleEndian.Uint16(p.data[base+6:]),
	}
}

func (p *page) writeSlot(index int, s slot) {
	base := slotBase(index)
	binary.LittleEndian.PutUint16(p.data[base:], s.offset)
	binary.LittleEndian.PutUint16(p.data[base+2:], s.keyLen)
	binary.LittleEndian.PutUint16(p.data[base+4:], s.valueLen)
	binary.LittleEndian.PutUint16(p.data[base+6:], s.flags)
}

func (p *page) readCell(index int) (string, []byte) {
	slot := p.readSlot(index)
	keyStart := int(slot.offset)
	keyEnd := keyStart + int(slot.keyLen)
	valueEnd := keyEnd + int(slot.valueLen)
	value := make([]byte, slot.valueLen)
	copy(value, p.data[keyEnd:valueEnd])
	return string(p.data[keyStart:keyEnd]), value
}

func (p *page) readCellKey(index int) string {
	slot := p.readSlot(index)
	keyStart := int(slot.offset)
	keyEnd := keyStart + int(slot.keyLen)
	return string(p.data[keyStart:keyEnd])
}

func (p *page) readCellValue(index int) []byte {
	slot := p.readSlot(index)
	valueStart := int(slot.offset) + int(slot.keyLen)
	valueEnd := valueStart + int(slot.valueLen)
	value := make([]byte, slot.valueLen)
	copy(value, p.data[valueStart:valueEnd])
	return value
}

func (p *page) readCellPageID(index int) PageID {
	slot := p.readSlot(index)
	valueStart := int(slot.offset) + int(slot.keyLen)
	return decodePageID(p.data[valueStart : valueStart+int(slot.valueLen)])
}

func (p *page) branchChild(index int) PageID {
	if index == 0 {
		return p.leftmostChild()
	}
	return p.readCellPageID(index - 1)
}

func (p *page) compareCellKey(index int, key string) int {
	slot := p.readSlot(index)
	keyStart := int(slot.offset)
	keyBytes := p.data[keyStart : keyStart+int(slot.keyLen)]

	for i := 0; i < len(keyBytes) && i < len(key); i++ {
		switch {
		case keyBytes[i] < key[i]:
			return -1
		case keyBytes[i] > key[i]:
			return 1
		}
	}

	switch {
	case len(keyBytes) < len(key):
		return -1
	case len(keyBytes) > len(key):
		return 1
	default:
		return 0
	}
}

func (p *page) searchLeafValue(key string) ([]byte, bool) {
	value, _, found := p.searchLeafCell(key)
	if !found {
		return nil, false
	}
	return value, true
}

func (p *page) searchLeafCell(key string) ([]byte, uint16, bool) {
	index, found := p.searchSlot(key)
	if !found {
		return nil, 0, false
	}
	return p.readCellValue(index), p.readSlot(index).flags, true
}

func (p *page) overflowRef(key string) (overflowRef, bool) {
	raw, flags, found := p.searchLeafCell(key)
	if !found {
		return overflowRef{}, false
	}
	return decodeOverflowRef(raw, flags)
}

func (p *page) searchBranchChild(key string) PageID {
	index, found := p.searchSlot(key)
	if found {
		return p.readCellPageID(index)
	}
	if index == 0 {
		return p.leftmostChild()
	}
	return p.readCellPageID(index - 1)
}

func (p *page) searchSlot(key string) (int, bool) {
	low, high := 0, int(p.slotCount())
	for low < high {
		mid := low + (high-low)/2
		switch cmp := p.compareCellKey(mid, key); {
		case cmp < 0:
			low = mid + 1
		case cmp > 0:
			high = mid
		default:
			return mid, true
		}
	}
	return low, false
}

func (p *page) appendCell(key string, value []byte) bool {
	return p.appendCellWithFlags(key, value, 0)
}

func (p *page) appendCellWithFlags(key string, value []byte, flags uint16) bool {
	cellSize := len(key) + len(value)
	needed := slotSize + cellSize
	if int(p.freeUpper())-(pageHeaderSize+int(p.slotCount())*slotSize) < needed {
		return false
	}

	cellOffset := int(p.freeUpper()) - cellSize
	copy(p.data[cellOffset:], key)
	copy(p.data[cellOffset+len(key):], value)

	slotIndex := int(p.slotCount())
	p.writeSlot(slotIndex, slot{
		offset:   uint16(cellOffset),
		keyLen:   uint16(len(key)),
		valueLen: uint16(len(value)),
		flags:    flags,
	})
	p.setFreeUpper(cellOffset)
	p.setSlotCount(uint16(slotIndex + 1))
	p.updateChecksum()
	return true
}

func (p *page) reset(flags uint16) {
	clear(p.data)
	p.setFlags(flags)
	p.setSlotCount(0)
	p.setFreeUpper(PageSize)
	p.updateChecksum()
}

func cloneBytes(value []byte) []byte {
	if value == nil {
		return nil
	}
	out := make([]byte, len(value))
	copy(out, value)
	return out
}

func encodePageID(out []byte, id PageID) {
	binary.LittleEndian.PutUint64(out, uint64(id))
}

func decodePageID(in []byte) PageID {
	return PageID(binary.LittleEndian.Uint64(in))
}

func encodePageIDValue(id PageID) []byte {
	out := make([]byte, 8)
	encodePageID(out, id)
	return out
}

func cloneValues(values [][]byte) [][]byte {
	out := make([][]byte, len(values))
	for i, value := range values {
		out[i] = cloneBytes(value)
	}
	return out
}

func (p *page) leafEntries() []leafEntry {
	entries := make([]leafEntry, 0, p.slotCount())
	for i := 0; i < int(p.slotCount()); i++ {
		slot := p.readSlot(i)
		key, value := p.readCell(i)
		entries = append(entries, leafEntry{key: key, value: value, encoded: true, slotFlags: slot.flags})
	}
	return entries
}

func (p *page) branchParts() ([]string, []PageID) {
	keys := make([]string, 0, p.slotCount())
	children := make([]PageID, 0, int(p.slotCount())+1)
	children = append(children, p.leftmostChild())
	for i := 0; i < int(p.slotCount()); i++ {
		key, encodedChild := p.readCell(i)
		keys = append(keys, key)
		children = append(children, decodePageID(encodedChild))
	}
	return keys, children
}

func (p *page) childIDs() []PageID {
	if !p.isBranch() {
		return nil
	}
	_, children := p.branchParts()
	return children
}

func normalizeDegree(degree int) int {
	if degree < 2 {
		return 2
	}
	return degree
}

func maxKeys(degree int) int {
	return 2*degree - 1
}
