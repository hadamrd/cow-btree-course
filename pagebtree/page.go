package pagebtree

import "encoding/binary"

// PageSize is the teaching target used in the docs. This package keeps page
// contents in a fixed byte array so the code demonstrates the usual slotted
// page shape: header and slots grow right, cells grow left.
const PageSize = 4096

type PageID uint64

const (
	pageHeaderSize = 16
	slotSize       = 8

	headerFlagsOffset     = 0
	headerSlotCountOffset = 2
	headerFreeUpperOffset = 4
	headerLeftmostOffset  = 8

	flagLeaf   = uint16(0x01)
	flagBranch = uint16(0x02)
)

type slot struct {
	offset   uint16
	keyLen   uint16
	valueLen uint16
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

func slotBase(index int) int {
	return pageHeaderSize + index*slotSize
}

func (p *page) readSlot(index int) slot {
	base := slotBase(index)
	return slot{
		offset:   binary.LittleEndian.Uint16(p.data[base:]),
		keyLen:   binary.LittleEndian.Uint16(p.data[base+2:]),
		valueLen: binary.LittleEndian.Uint16(p.data[base+4:]),
	}
}

func (p *page) writeSlot(index int, s slot) {
	base := slotBase(index)
	binary.LittleEndian.PutUint16(p.data[base:], s.offset)
	binary.LittleEndian.PutUint16(p.data[base+2:], s.keyLen)
	binary.LittleEndian.PutUint16(p.data[base+4:], s.valueLen)
	binary.LittleEndian.PutUint16(p.data[base+6:], 0)
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

func (p *page) appendCell(key string, value []byte) bool {
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
	})
	p.setFreeUpper(cellOffset)
	p.setSlotCount(uint16(slotIndex + 1))
	return true
}

func (p *page) reset(flags uint16) {
	clear(p.data)
	p.setFlags(flags)
	p.setSlotCount(0)
	p.setFreeUpper(PageSize)
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
		key, value := p.readCell(i)
		entries = append(entries, leafEntry{key: key, value: value})
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
