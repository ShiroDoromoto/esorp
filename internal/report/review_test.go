package report

import (
	"strings"
	"testing"

	"github.com/ShiroDoromoto/esorp/internal/audit"
	"github.com/ShiroDoromoto/esorp/internal/place"
	"github.com/ShiroDoromoto/esorp/internal/scan"
)

// review1 は、層1・層2 を通り抜けたコメントと、それらに投げる問い。
func review1() *audit.Review {
	return &audit.Review{
		Question: "このコメントは、コードから読み取れない事情を語っていますか。\n",
		Comments: []audit.Passed{{
			Path: "internal/store/index.go",
			Comment: place.Comment{
				Kind: scan.KindDocLine, Place: place.Doc, Line: 4, Col: 1,
				Text: "// Index は、鍵から位置を引く。",
			},
		}},
	}
}

// TestReviewText は、件数と問いだけを言い、判定を書かないことを見る（答えるのは esorp ではない）。
func TestReviewText(t *testing.T) {
	var b strings.Builder
	if err := ReviewText(&b, review1()); err != nil {
		t.Fatal(err)
	}

	wants(t, b.String(), `internal/store/index.go:4:1  place=doc kind=docline
  // Index は、鍵から位置を引く。

1 comments passed layers 1 and 2.

  このコメントは、コードから読み取れない事情を語っていますか。

The one who answers is not esorp — it is the agent reading this output.
`)
}

func TestReviewJSON(t *testing.T) {
	var b strings.Builder
	if err := ReviewJSON(&b, review1()); err != nil {
		t.Fatal(err)
	}

	wants(t, b.String(), `{
  "version": 1,
  "question": "このコメントは、コードから読み取れない事情を語っていますか。",
  "summary": {
    "comments": 1
  },
  "comments": [
    {
      "path": "internal/store/index.go",
      "line": 4,
      "col": 1,
      "place": "doc",
      "kind": "docline",
      "text": "// Index は、鍵から位置を引く。"
    }
  ]
}
`)
}

// TestReviewShapeMatchesCheck は、esorp review と check --diff --format json の review が、同じ形の
// コメントを出すことを見る（答える側が入口を1つ覚えれば足りるように）。
func TestReviewShapeMatchesCheck(t *testing.T) {
	rv := review1()

	var review, check strings.Builder
	if err := ReviewJSON(&review, rv); err != nil {
		t.Fatal(err)
	}
	if err := JSON(&check, &audit.Result{Files: 1, Comments: 1, Review: rv}); err != nil {
		t.Fatal(err)
	}

	var r struct {
		Question string        `json:"question"`
		Comments []jsonComment `json:"comments"`
	}
	var c struct {
		Review struct {
			Question string        `json:"question"`
			Comments []jsonComment `json:"comments"`
		} `json:"review"`
	}
	decode(t, review.String(), &r)
	decode(t, check.String(), &c)

	if r.Question != c.Review.Question {
		t.Errorf("問いが食い違っています:\nreview = %q\ncheck  = %q", r.Question, c.Review.Question)
	}
	if len(r.Comments) != 1 || len(c.Review.Comments) != 1 || r.Comments[0] != c.Review.Comments[0] {
		t.Errorf("コメントの形が食い違っています:\nreview = %+v\ncheck  = %+v", r.Comments, c.Review.Comments)
	}
}
