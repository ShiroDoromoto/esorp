package report

import (
	"io"

	"github.com/ShiroDoromoto/esorp/internal/config"
)

// jsonDiff は init --diff の機械可読の形。差分を読むのは人とはかぎらず、エージェントが手元の設定を
// 更新することもある。書き換えるかどうかを決めるのは相変わらず読み手で、esorp は設定に触らない。
type jsonDiff struct {
	Version  int           `json:"version"`
	Config   string        `json:"config"`
	Same     bool          `json:"same"`
	Sections []jsonSection `json:"sections"`
}

type jsonSection struct {
	Title   string       `json:"title"`
	Changes []jsonChange `json:"changes"`
}

// jsonChange は差分1つ。key は設定の該当箇所まで辿れる名前（syntax.cstyle.allow[doc].kind）、
// only は片方にしか無いこと（local / template）、text は check / explain の JSON と同じく、
// 人向けの1行をそのまま添えたもの。
type jsonChange struct {
	Key   string `json:"key"`
	Local string `json:"local,omitempty"`
	Tmpl  string `json:"template,omitempty"`
	Only  string `json:"only,omitempty"`
	Text  string `json:"text"`
}

// DiffJSON は、現行テンプレートと手元の設定の差分を機械可読で書く。
func DiffJSON(w io.Writer, configPath string, sections []config.Section) error {
	out := jsonDiff{
		Version:  1,
		Config:   configPath,
		Same:     len(sections) == 0,
		Sections: make([]jsonSection, 0, len(sections)),
	}
	for _, s := range sections {
		sec := jsonSection{Title: s.Title, Changes: make([]jsonChange, 0, len(s.Changes))}
		for _, c := range s.Changes {
			sec.Changes = append(sec.Changes, jsonChange{
				Key:   c.Key,
				Local: c.Local,
				Tmpl:  c.Tmpl,
				Only:  c.Only,
				Text:  c.Text,
			})
		}
		out.Sections = append(out.Sections, sec)
	}

	return encode(w, out)
}
