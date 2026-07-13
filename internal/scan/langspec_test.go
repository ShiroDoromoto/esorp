package scan

import "testing"

// TestSpecByName は、名乗っている字句がすべて名前で引けること、そのファミリも引けることを確かめる。
// 設定の lang: はこの名前で書くので、引けない名前が混じると、書けるはずの指定が設定エラーになる。
func TestSpecByName(t *testing.T) {
	names := LangNames()
	if len(names) == 0 {
		t.Fatal("名指しできる字句が1つも無い")
	}

	for _, name := range names {
		spec, ok := SpecByName(name)
		if !ok {
			t.Errorf("SpecByName(%q) が引けない", name)
			continue
		}
		if spec.Name != name {
			t.Errorf("SpecByName(%q).Name = %q", name, spec.Name)
		}
		if _, ok := LangFamily(name); !ok {
			t.Errorf("LangFamily(%q) が引けない", name)
		}
	}

	if _, ok := SpecByName("golang"); ok {
		t.Error(`SpecByName("golang") が引けた（無い字句は引けてはならない）`)
	}
}
