package pagebtree

import (
	"encoding/binary"
	"errors"
)

const (
	overflowRefMagic      = "OVF1"
	overflowRefSize       = 20
	overflowPayloadOffset = pageHeaderSize
	overflowPayloadSize   = PageSize - overflowPayloadOffset
	slotFlagOverflow      = uint16(0x01)
)

type overflowRef struct {
	first  PageID
	length int
}

var ErrOverflowInvariant = errors.New("overflow invariant invalid")

func encodeOverflowRef(ref overflowRef) []byte {
	out := make([]byte, overflowRefSize)
	copy(out, overflowRefMagic)
	binary.LittleEndian.PutUint64(out[4:], uint64(ref.first))
	binary.LittleEndian.PutUint64(out[12:], uint64(ref.length))
	return out
}

func decodeOverflowRef(value []byte, flags uint16) (overflowRef, bool) {
	if flags&slotFlagOverflow == 0 {
		return overflowRef{}, false
	}
	if len(value) != overflowRefSize || string(value[:4]) != overflowRefMagic {
		return overflowRef{}, false
	}
	return overflowRef{
		first:  PageID(binary.LittleEndian.Uint64(value[4:])),
		length: int(binary.LittleEndian.Uint64(value[12:])),
	}, true
}

func (t *Tree) leafCellValue(key string, value []byte) ([]byte, uint16) {
	if !shouldStoreOverflow(key, value) {
		return cloneBytes(value), 0
	}
	return t.overflowCellValue(value), slotFlagOverflow
}

func (t *Tree) overflowCellValue(value []byte) []byte {
	return encodeOverflowRef(overflowRef{
		first:  t.writeOverflowChain(value),
		length: len(value),
	})
}

func shouldStoreOverflow(key string, value []byte) bool {
	return len(key)+len(value)+slotSize > PageSize/2
}

func (t *Tree) writeOverflowChain(value []byte) PageID {
	pageCount := (len(value) + overflowPayloadSize - 1) / overflowPayloadSize
	ids := make([]PageID, pageCount)
	for i := range ids {
		ids[i] = t.allocPage()
	}

	for i, id := range ids {
		next := PageID(0)
		if i+1 < len(ids) {
			next = ids[i+1]
		}
		start := i * overflowPayloadSize
		end := start + overflowPayloadSize
		if end > len(value) {
			end = len(value)
		}
		p := t.newPage(id, flagOverflow)
		writeOverflowPage(p, next, value[start:end])
		t.pages[id] = p
	}
	return ids[0]
}

func writeOverflowPage(p *page, next PageID, chunk []byte) {
	if len(chunk) > overflowPayloadSize {
		panic("overflow page chunk is too large")
	}
	p.reset(flagOverflow)
	p.setLeftmostChild(next)
	p.setOverflowPayloadLen(len(chunk))
	copy(p.data[overflowPayloadOffset:], chunk)
	p.updateChecksum()
}

func (p *page) overflowNext() PageID {
	return p.leftmostChild()
}

func (p *page) overflowPayloadLen() int {
	return int(p.slotCount())
}

func (p *page) setOverflowPayloadLen(length int) {
	p.setSlotCount(uint16(length))
}

func (p *page) overflowPayload() []byte {
	return p.data[overflowPayloadOffset : overflowPayloadOffset+p.overflowPayloadLen()]
}

func resolveLeafValue(pages map[PageID]*page, raw []byte, flags uint16) []byte {
	ref, ok := decodeOverflowRef(raw, flags)
	if !ok {
		return cloneBytes(raw)
	}
	return readOverflowChain(pages, ref)
}

func readOverflowChain(pages map[PageID]*page, ref overflowRef) []byte {
	out := make([]byte, 0, ref.length)
	for id := ref.first; id != 0; {
		p := pages[id]
		out = append(out, p.overflowPayload()...)
		id = p.overflowNext()
	}
	if len(out) > ref.length {
		out = out[:ref.length]
	}
	return out
}

func (t *Tree) retireOverflowValue(raw []byte, flags uint16) {
	ref, ok := decodeOverflowRef(raw, flags)
	if !ok {
		return
	}
	for id := ref.first; id != 0; {
		p := t.pages[id]
		next := p.overflowNext()
		t.retirePage(id)
		id = next
	}
}
