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

// TestLangsUnique は、登録簿に同じ名前・同じ拡張子・同じファイル名が二度現れないことを確かめる。
// 重複しても引く側は先に見つけた方を返すだけなので、後から書いた行は黙って死ぬ。
func TestLangsUnique(t *testing.T) {
	seen := map[string]string{}
	claim := func(kind, key, by string) {
		if prev, dup := seen[kind+":"+key]; dup {
			t.Errorf("%s %q を %s と %s が二重に登録している", kind, key, prev, by)
			return
		}
		seen[kind+":"+key] = by
	}

	for _, l := range langs {
		name := l.spec().Name
		if name == "" {
			t.Errorf("family %q に名前の無い字句がある", l.family)
			continue
		}
		claim("字句", name, name)
		for _, ext := range l.exts {
			claim("拡張子", ext, name)
		}
		for _, file := range l.names {
			claim("ファイル名", file, name)
		}
		if l.familyDefault {
			claim("ファミリの既定", l.family, name)
		}
	}
}

// TestSpecForResolvesByName は、拡張子・ファイル名から引ける字句が、すべて lang: でも名指しできる
// ことを確かめる。この2つが食い違うと、拡張子では読めるのに設定から名指しできない字句ができる。
func TestSpecForResolvesByName(t *testing.T) {
	for _, l := range langs {
		for _, path := range append(append([]string{}, l.exts...), l.names...) {
			spec, ok := SpecFor(path)
			if !ok {
				t.Errorf("SpecFor(%q) が引けない（登録簿には %s として在る）", path, l.spec().Name)
				continue
			}
			if _, ok := SpecByName(spec.Name); !ok {
				t.Errorf("SpecFor(%q) = %q は SpecByName で引けない", path, spec.Name)
			}
		}
	}
}
