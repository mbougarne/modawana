package theme

// Reading is the collection index, outline or reader view.
type Reading struct {
	Kind                string
	Title, Intro, Empty string
	Counts              string
	Progress            bool
	Collections         []Collection
	CollectionTitle     string
	Pager               Pager
	Outline             *Outline
	Reader              *Article
	Neighbors           *Neighbors
	DocumentError       string
}

type Pager struct {
	Label, Previous, PreviousURL, Next, NextURL string
}

type Outline struct {
	Title, Label, Progress string
	Items                  []OutlineItem
	Pager                  Pager
	Refresh                bool
}

type OutlineItem struct {
	Title, URL, Status, State string
	Position                  int
	Current                   bool
}

type Neighbors struct {
	Label   string
	Items   []Neighbor
	Refresh bool
}

type Neighbor struct {
	Title, URL, Label string
	Next              bool
}

type LockedLesson struct {
	Title, Help, Unavailable, Prerequisite string
	Links                                  []Language
}

type LearningMount struct {
	Key, Kind, Locale string
}
