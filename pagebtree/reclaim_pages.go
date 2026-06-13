package pagebtree

import "encoding/binary"

const (
	reclaimPayloadOffset = pageHeaderSize
	reclaimRecordSize    = 16
	reclaimPageCapacity  = (PageSize - reclaimPayloadOffset) / reclaimRecordSize
)

type reclaimRecord struct {
	id       PageID
	revision uint64
}

func writeReclaimPage(p *page, next PageID, records []reclaimRecord) {
	p.reset(flagReclaim)
	p.setLeftmostChild(next)
	p.setReclaimCount(len(records))
	for i, record := range records {
		offset := reclaimPayloadOffset + i*reclaimRecordSize
		binary.LittleEndian.PutUint64(p.data[offset:], uint64(record.id))
		binary.LittleEndian.PutUint64(p.data[offset+8:], record.revision)
	}
	p.updateChecksum()
}

func (p *page) reclaimNext() PageID {
	return p.leftmostChild()
}

func (p *page) reclaimCount() int {
	return int(p.slotCount())
}

func (p *page) setReclaimCount(count int) {
	p.setSlotCount(uint16(count))
}

func (p *page) reclaimRecords() []reclaimRecord {
	records := make([]reclaimRecord, 0, p.reclaimCount())
	for i := 0; i < p.reclaimCount(); i++ {
		offset := reclaimPayloadOffset + i*reclaimRecordSize
		records = append(records, reclaimRecord{
			id:       PageID(binary.LittleEndian.Uint64(p.data[offset:])),
			revision: binary.LittleEndian.Uint64(p.data[offset+8:]),
		})
	}
	return records
}
