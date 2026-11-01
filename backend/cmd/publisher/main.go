package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/publisher"
	"io"
	"os"
	"regexp"
	"time"
)

var keyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,119}$`)

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: publisher get|draft|publish|upload|deliver|schedule [flags]; credentials: BLOG_URL, BLOG_PUBLISHER_TOKEN")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	stateDir := flags.String("state-dir", "", "private persistent delivery directory")
	key := flags.String("key", "", "stable content key")
	locale := flags.String("locale", "en", "content locale")
	input := flags.String("input", "", "draft JSON file")
	markdown := flags.String("markdown", "", "Markdown body file")
	file := flags.String("file", "", "upload file")
	mime := flags.String("mime", "", "upload MIME type")
	retry := flags.String("idempotency", "", "stable retry key")
	revision := flags.String("revision", "", "exact revision to publish")
	expected := flags.Int64("expected-version", 0, "current content version")
	if e := flags.Parse(args[1:]); e != nil || flags.NArg() != 0 {
		return errors.New("invalid publisher flags")
	}
	if args[0] != "upload" && args[0] != "deliver" && args[0] != "schedule" && (!keyPattern.MatchString(*key) || (*locale != "en" && *locale != "ar" && *locale != "fr")) {
		return errors.New("valid --key and --locale are required")
	}
	if args[0] != "get" && args[0] != "deliver" && args[0] != "schedule" && *retry == "" {
		return errors.New("--idempotency is required; reuse it for an interrupted retry")
	}
	var ca []byte
	var e error
	if path := os.Getenv("BLOG_PUBLISHER_CA"); path != "" {
		ca, e = os.ReadFile(path)
		if e != nil {
			return errors.New("cannot read publisher CA file")
		}
	}
	client, e := publisher.New(os.Getenv("BLOG_URL"), os.Getenv("BLOG_PUBLISHER_TOKEN"), ca)
	if e != nil {
		return e
	}
	ctx := context.Background()
	if args[0] == "schedule" {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		return publisher.RunSchedule(ctx, client, publisher.NewNotion(os.Getenv("NOTION_API_KEY")), publisher.NewLessonGenerator(os.Getenv("OPENAI_API_KEY")), *input, *stateDir, time.Now())
	}
	if args[0] == "deliver" {
		f, e := os.Open(*input)
		if e != nil {
			return errors.New("cannot open delivery manifest")
		}
		defer f.Close()
		var manifest publisher.Delivery
		decoder := json.NewDecoder(io.LimitReader(f, 2<<20))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&manifest) != nil {
			return errors.New("invalid delivery manifest")
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			return errors.New("trailing delivery data")
		}
		state, e := publisher.Deliver(ctx, client, publisher.NewNotion(os.Getenv("NOTION_API_KEY")), *stateDir, manifest)
		json.NewEncoder(os.Stdout).Encode(state)
		return e
	}
	path := "/api/v1/publisher/content/" + *key + "/" + *locale
	var out json.RawMessage
	switch args[0] {
	case "get":
		out, e = client.Do(ctx, "GET", path, nil, "")
	case "draft":
		f, err := os.Open(*input)
		if err != nil {
			return errors.New("cannot open draft JSON file")
		}
		defer f.Close()
		decoder := json.NewDecoder(io.LimitReader(f, 2<<20))
		decoder.DisallowUnknownFields()
		var in content.DraftInput
		if err = decoder.Decode(&in); err != nil {
			return errors.New("invalid draft JSON")
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			return errors.New("draft JSON has trailing data")
		}
		if *markdown != "" {
			b, err := os.ReadFile(*markdown)
			if err != nil || len(b) > content.MaxBody {
				return errors.New("cannot read bounded Markdown file")
			}
			in.Markdown = string(b)
		}
		out, e = client.Do(ctx, "PUT", path, in, *retry)
	case "publish":
		out, e = client.Do(ctx, "POST", path+"/publish", content.Action{ExpectedVersion: expected, RevisionID: *revision}, *retry)
	case "upload":
		f, err := os.Open(*file)
		if err != nil {
			return errors.New("cannot open upload file")
		}
		defer f.Close()
		out, e = client.Upload(ctx, *file, *mime, f, *retry)
	default:
		return errors.New("unknown publisher command")
	}
	if e != nil {
		return e
	}
	fmt.Println(string(out))
	return nil
}
func main() {
	if e := run(os.Args[1:]); e != nil {
		var api *publisher.APIError
		if errors.As(e, &api) {
			json.NewEncoder(os.Stderr).Encode(api)
		} else {
			fmt.Fprintln(os.Stderr, e.Error())
		}
		os.Exit(1)
	}
}
