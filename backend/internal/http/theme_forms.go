package httpserver

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"html/template"

	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/forms"
)

var formHTML = template.Must(template.New("lead-form").Parse(`
<form class="lead-form" method="post" action="/api/v1/public/lead-submit" data-lead-form
 data-accepted="{{.Accepted}}" data-failed="{{.Failed}}">
 <fieldset{{if .Preview}} disabled{{end}}>
 <legend dir="auto">{{.Form.Title}}</legend>
 {{range .Form.Fields}}
 <label class="config-field"><span dir="auto">{{.Label}}</span>
 {{if eq .Kind "textarea"}}<textarea name="f_{{.Key}}" maxlength="2000" dir="auto"{{if .Required}} required{{end}}></textarea>
 {{else}}<input type="{{.Kind}}" name="f_{{.Key}}" maxlength="300" dir="auto"{{if .Required}} required{{end}}>{{end}}
 </label>
 {{end}}
 {{if .Form.Consent}}<label><input type="checkbox" name="consent" value="yes" required> <span dir="auto">{{.Form.Consent}}</span></label>{{end}}
 <div hidden><input name="website" type="text" tabindex="-1" autocomplete="off"></div>
 <input name="form" type="hidden" value="{{.Ref.Key}}">
 <input name="version" type="hidden" value="{{.Ref.Version}}">
 <input name="page" type="hidden" value="{{.Page.Key}}">
 <input name="locale" type="hidden" value="{{.Page.Locale}}">
 <input name="revision" type="hidden" value="{{.Page.RevisionID}}">
 <input name="block" type="hidden" value="{{.Block}}">
 <input name="retry" type="hidden" value="{{.Retry}}">
 <button type="submit" class="vb-btn">{{.Send}}</button>
 </fieldset>
 <p role="status"></p>
</form>
`))

func (s *Server) themeLeadForm(ctx context.Context, page *content.PublicContent, head *headData, label func(string) string, block string, ref forms.Reference) (template.HTML, error) {
	domain, ok := s.options.Domain.(*Domain)
	if !ok || domain.Forms == nil {
		return "", nil
	}
	def, err := forms.Visible(ctx, domain.Forms.DB, ref)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	retry, err := db.NewID()
	if err != nil {
		return "", err
	}
	var output bytes.Buffer
	err = formHTML.Execute(&output, struct {
		Form                                 forms.Definition
		Ref                                  forms.Reference
		Page                                 *content.PublicContent
		Preview                              bool
		Block, Retry, Send, Accepted, Failed string
	}{def, ref, page, head.Canonical == "", block, retry, label("forms.send"), label("forms.accepted"), label("forms.failed")})
	return template.HTML(output.String()), err
}
