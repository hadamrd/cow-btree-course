package pagebtree

type Snapshot struct {
	tree     *Tree
	pages    map[PageID]*page
	root     PageID
	length   int
	revision uint64
	degree   int
	closed   bool
}

func (s *Snapshot) Len() int {
	if s == nil {
		return 0
	}
	return s.length
}

func (s *Snapshot) Revision() uint64 {
	if s == nil {
		return 0
	}
	return s.revision
}

func (s *Snapshot) Get(key string) ([]byte, bool) {
	if s == nil || s.closed {
		return nil, false
	}
	return searchPage(s.pages, s.root, key)
}

func (s *Snapshot) Range(visit func(string, []byte) bool) {
	if s == nil || s.closed {
		return
	}
	rangePage(s.pages, s.root, visit)
}

func (s *Snapshot) RangeFrom(start string, visit func(string, []byte) bool) {
	if s == nil || s.closed {
		return
	}
	rangePageFrom(s.pages, s.root, start, visit)
}

func (s *Snapshot) Stats() Stats {
	if s == nil {
		return Stats{}
	}
	return statsForSnapshot(s.pages, s.root, s.length, s.revision, s.degree)
}

func (s *Snapshot) Close() {
	if s == nil || s.closed {
		return
	}
	s.closed = true
	tree := s.tree
	if tree != nil {
		tree.endRead(s.revision)
		if !tree.closed && !tree.readOnly {
			tree.relinkLeaves()
		}
	}
}
