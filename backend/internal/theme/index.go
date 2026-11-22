package theme

import "html/template"

// Text carries profile text with the language used after fallback.
type Text struct {
	Value, Lang, Dir string
}

type Portrait struct {
	URL, Alt, Monogram string
	Sticker            Text
	RTL                bool
}

type Action struct {
	Text    Text
	URL     string
	Icon    string
	Variant string
}

type Banner struct {
	URL string
	Alt string
}

type NowRow struct {
	Label, Text Text
}

type Collection struct {
	Title, URL, Summary, Counts, Initials string
	Badge                                 int
}

type Home struct {
	Heading                             Text
	Headline                            [][]BioSegment
	Bio                                 Text
	BioSegments                         []BioSegment
	Banner                              *Banner
	Portrait                            *Portrait
	Actions                             []Action
	Now                                 []NowRow
	NowTitle, UpdatedMonth, UpdatedText string
	WritingTitle, LearningTitle         string
	WritingURL, LearningURL             string
	MoreLabel, Arrow, Empty             string
	Posts                               []Year
	Collections                         []Collection
}

type Experience struct {
	Role, Organization, Description Text
	Start, End, Current, URL        string
}

type About struct {
	Title, ExperienceLabel, WebsiteLabel string
	Bio, Location                        Text
	BioSegments                          []BioSegment
	Portrait                             Portrait
	Experience                           []Experience
	Body                                 template.HTML
	Actions                              []Action
	Email, Copy, Copied, CopyFailed      string
}

type Post struct {
	Title, Summary, URL, DateISO, DateText string
	Badge, Kind, LockLabel                 string
	Tags                                   []string
}

type Year struct {
	Number string
	Posts  []Post
}

type Choice struct {
	Value, Label string
	Selected     bool
}

type Filter struct {
	Link
	Count *int
}

type FilterGroup struct {
	Label string
	Tag   bool
	Items []Filter
}

type Field struct {
	Name, Value string
}

type Discovery struct {
	Title, Intro, URL, Query, QueryLabel, Submit, KindLabel string
	Search                                                  bool
	Kinds                                                   []Choice
	Fields                                                  []Field
	Filters                                                 []FilterGroup
	Active, Clear                                           string
	Count, Empty, Problem, RetryLabel, RetryURL, LiveError  string
	PagesLabel, Next, Previous, NextURL, PreviousURL        string
	Posts                                                   []Year
}
