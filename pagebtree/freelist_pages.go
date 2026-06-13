package pagebtree

import "encoding/binary"

const (
	freelistPayloadOffset = pageHeaderSize
	freelistPageCapacity  = (PageSize - freelistPayloadOffset) / 8
)

func writeFreelistPage(p *page, next PageID, ids []PageID) {
	p.reset(flagFreelist)
	p.setLeftmostChild(next)
	p.setFreelistCount(len(ids))
	for i, id := range ids {
		offset := freelistPayloadOffset + i*8
		binary.LittleEndian.PutUint64(p.data[offset:], uint64(id))
	}
	p.updateChecksum()
}

func (p *page) freelistNext() PageID {
	return p.leftmostChild()
}

func (p *page) freelistCount() int {
	return int(p.slotCount())
}

func (p *page) setFreelistCount(count int) {
	p.setSlotCount(uint16(count))
}

func (p *page) freelistIDs() []PageID {
	ids := make([]PageID, 0, p.freelistCount())
	for i := 0; i < p.freelistCount(); i++ {
		offset := freelistPayloadOffset + i*8
		ids = append(ids, PageID(binary.LittleEndian.Uint64(p.data[offset:])))
	}
	return ids
}
