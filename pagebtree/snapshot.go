package pagebtree

type Snapshot struct {
	pages    map[PageID]*page
	root     PageID
	length   int
	revision uint64
	degree   int
}

func (s Snapshot) Len() int {
	return s.length
}

func (s Snapshot) Revision() uint64 {
	return s.revision
}

func (s Snapshot) Get(key string) ([]byte, bool) {
	return searchPage(s.pages, s.root, key)
}

func (s Snapshot) Range(visit func(string, []byte) bool) {
	rangePage(s.pages, s.root, visit)
}

func (s Snapshot) Stats() Stats {
	return statsFor(s.pages, s.root, s.length, s.revision, s.degree)
}
