#!/usr/bin/env bash
# Execute the rendered helper against real Git objects, outside Go tests.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
scratch="$(mktemp "$root/pii-helper-XXXXXX.go")"
tmp="$(mktemp -d)"
trap 'rm -f "$scratch"; rm -rf "$tmp"' EXIT
cat > "$scratch" <<'GO'
package main
import (
  "os"
  "github.com/jpvelasco/fundamentum/internal/templates"
)
func main() {
  files, err := templates.Render(templates.RepoData{Owner:"o", RepoName:"r", DefaultBranch:"main", Visibility:"public", CIPack:"none"})
  if err != nil { panic("render failed") }
  for _, file := range files {
    if file.Path == ".github/scripts/pii-gate.go" { _, _ = os.Stdout.WriteString(file.Content); return }
  }
  panic("helper not rendered")
}
GO
(cd "$root"; go run "$scratch") > "$tmp/helper.go"
rm -f "$scratch"
git init -q -b main "$tmp/tree"
cd "$tmp/tree"
git config user.name "Helper verification"
git config user.email "verification@example.invalid"
id="42""4242424242"
mkdir -p .github
printf '%s\n' '# reviewed empty policy' > .github/pii-allowlist.txt
printf '%s\n' '123456789012' > synthetic
mkdir representative
for n in {1..100}; do
  { printf 'file %s\n' "$n"; head -c 4096 /dev/zero; } > "representative/$n"
done
git add .
git commit -qm "test: clean baseline"

scan() {
  git ls-tree -r -z --full-tree HEAD > "$tmp/manifest"
  git ls-tree -r -z --format='%(objecttype) %(objectname)' HEAD |
    while IFS=' ' read -r -d '' type sha; do
      if [ "$type" = blob ]; then printf '%s\n' "$sha"; fi
    done > "$tmp/objects"
  git cat-file --batch < "$tmp/objects" > "$tmp/batch"
  result=0
  GO111MODULE=off GOWORK=off GOTOOLCHAIN=local GOCACHE="$tmp/cache" \
    go run "$tmp/helper.go" "$tmp/manifest" "$tmp/batch" > "$tmp/out" 2>&1 || result=$?
  if [ "$result" -ne "$1" ]; then echo "helper scenario failed: $2"; exit 1; fi
  if grep -qF "$id" "$tmp/out"; then echo "helper leaked a matched value"; exit 1; fi
}
start="$(date +%s)"
scan 0 clean
elapsed="$(( $(date +%s) - start ))"
printf 'Rendered helper cold clean run: %ss\n' "$elapsed"
if [ "$elapsed" -ge 60 ]; then
  echo "Cold helper run exceeded the 60s target"
fi
printf '%s\n' "$id" > untracked
scan 0 untracked
git switch -qc stale
printf '%s\n' "$id" > unchanged
git add unchanged
git commit -qm "test: stale branch content"
git switch -q main
printf '%s\n' clean > unrelated
git add unrelated
git commit -qm "test: unrelated change"
git merge -qm "test: merge stale content" stale
scan 1 stale-merge
grep -q 'unchanged.*:1: account-id' "$tmp/out"
git rm -q unchanged
printf '%s\n' 'exported export-ignore' > .gitattributes
printf '%s\n' ignored > .gitignore
printf '%s\n' "$id" > exported
printf '%s\n' "$id" > ignored
ln -s "$id" symlink
git add .gitattributes .gitignore exported symlink
git add -f ignored
git commit -qm "test: tracked scope"
scan 1 tracked-scope
for file in exported ignored symlink; do grep -q "$file.*:1: account-id" "$tmp/out"; done
printf '%s\n' "$id" >> .github/pii-allowlist.txt
git add .github/pii-allowlist.txt
git commit -qm "test: exact exemption"
scan 0 allowlisted
truncate -s -1 "$tmp/batch"
result=0
GO111MODULE=off GOWORK=off GOTOOLCHAIN=local GOCACHE="$tmp/cache" \
  go run "$tmp/helper.go" "$tmp/manifest" "$tmp/batch" > "$tmp/out" 2>&1 || result=$?
test "$result" -ne 0
echo "Rendered helper Git integration: passed"
test "$elapsed" -lt 60
