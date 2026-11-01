// Package access defines the shared authorization boundary for later domain tickets.
// Implementations must resolve current publication, path plan and learner epoch atomically.
package access

import "context"

type Audience uint8

const (
	Anonymous Audience = iota
	Learner
	Owner
	Publisher
)

type Principal struct {
	Audience      Audience
	ID            string
	Scopes        []string
	CollectionIDs []string
}
type Surface string

const (
	Content    Surface = "content"
	Media      Surface = "media"
	Preview    Surface = "preview"
	Search     Surface = "search"
	Feed       Surface = "feed"
	Collection Surface = "collection"
)

type Request struct {
	Principal  Principal
	Surface    Surface
	ResourceID string
	Locale     string
	Method     string
}
type Visibility uint8

const (
	Hidden Visibility = iota
	PublicTeaser
	Full
)

// The zero value denies access. A teaser never grants a body, quiz, or media response.
type Decision struct {
	Visibility  Visibility
	RevisionID  string
	PathVersion string
	Epoch       int64
}

func (d Decision) AllowsBody() bool { return d.Visibility == Full && d.RevisionID != "" }

type Service interface {
	Resolve(context.Context, Request) (Decision, error)
}

// DenyAll is the explicit foundation policy until the relevant domain service is installed.
type DenyAll struct{}

func (DenyAll) Resolve(context.Context, Request) (Decision, error) { return Decision{}, nil }
