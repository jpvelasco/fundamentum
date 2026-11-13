package github

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// TrackedBlob is a tracked path and its raw Git blob contents, including symlinks.
type TrackedBlob struct {
	Path string
	Data []byte
}

type gitTree struct {
	SHA       string `json:"sha"`
	Truncated bool   `json:"truncated"`
	Tree      []struct {
		Path string `json:"path"`
		Type string `json:"type"`
		SHA  string `json:"sha"`
	} `json:"tree"`
}

// safeTreeGet deliberately omits response bodies and decode input from errors.
// Cloud identifiers can occur in both, even on an unsuccessful request.
func (c *Client) safeTreeGet(path string, result any, missingOK bool) (bool, error) {
	quiet := *c
	quiet.Verbose = false
	resp, err := quiet.get(path)
	if err != nil {
		return false, fmt.Errorf("read tracked tree: request failed; verify access and retry")
	}
	defer func() { _ = resp.Body.Close() }()
	if missingOK && resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, &HTTPError{StatusCode: resp.StatusCode, Msg: fmt.Sprintf("read tracked tree: HTTP %d; verify access and retry", resp.StatusCode)}
	}
	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return false, fmt.Errorf("read tracked tree: invalid JSON")
	}
	return true, nil
}

// HasPIIPolicy is a cheap presence probe; enrollment validates the policy from
// the pinned tree, never from this potentially moving Contents API response.
func (c *Client) HasPIIPolicy(owner, repo string) (bool, error) {
	var result json.RawMessage
	return c.safeTreeGet(repoPath(owner, repo)+"/contents/.github/pii-allowlist.txt", &result, true)
}

// TrackedTree resolves a branch once, then reads every blob from that commit.
// Truncated recursive listings fall back to complete non-recursive traversal.
func (c *Client) TrackedTree(owner, repo, branch string) (string, []TrackedBlob, error) {
	base := repoPath(owner, repo)
	var commit struct {
		SHA    string `json:"sha"`
		Commit struct {
			Tree struct {
				SHA string `json:"sha"`
			} `json:"tree"`
		} `json:"commit"`
	}
	if _, err := c.safeTreeGet(base+"/commits/"+url.PathEscape(branch), &commit, false); err != nil {
		return "", nil, err
	}
	if commit.SHA == "" || commit.Commit.Tree.SHA == "" {
		return "", nil, fmt.Errorf("read tracked tree: missing commit or tree identity")
	}
	var recursive gitTree
	root := commit.Commit.Tree.SHA
	if _, err := c.safeTreeGet(base+"/git/trees/"+url.PathEscape(root)+"?recursive=1", &recursive, false); err != nil {
		return "", nil, err
	}
	if recursive.SHA != root || !recursive.Truncated && recursive.Tree == nil {
		return "", nil, fmt.Errorf("read tracked tree: tree identity mismatch")
	}
	blobs := map[string]string{}
	if recursive.Truncated {
		if err := c.walkTrackedTree(base, root, "", blobs, map[string]bool{}); err != nil {
			return "", nil, err
		}
	} else {
		for _, entry := range recursive.Tree {
			switch entry.Type {
			case "blob":
				if entry.Path == "" || entry.SHA == "" || blobs[entry.Path] != "" {
					return "", nil, fmt.Errorf("read tracked tree: invalid blob entry")
				}
				blobs[entry.Path] = entry.SHA
			case "tree", "commit":
			default:
				return "", nil, fmt.Errorf("read tracked tree: unknown object type")
			}
		}
	}
	paths := make([]string, 0, len(blobs))
	for path := range blobs {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	cache := map[string][]byte{}
	result := make([]TrackedBlob, 0, len(paths))
	for _, path := range paths {
		sha := blobs[path]
		data, ok := cache[sha]
		if !ok {
			var err error
			data, err = c.readTrackedBlob(base, sha)
			if err != nil {
				return "", nil, err
			}
			cache[sha] = data
		}
		result = append(result, TrackedBlob{Path: path, Data: data})
	}
	return commit.SHA, result, nil
}

func (c *Client) walkTrackedTree(base, sha, prefix string, blobs map[string]string, ancestors map[string]bool) error {
	if ancestors[sha] {
		return fmt.Errorf("read tracked tree: cyclic tree")
	}
	ancestors[sha] = true
	defer delete(ancestors, sha)
	var tree gitTree
	if _, err := c.safeTreeGet(base+"/git/trees/"+url.PathEscape(sha), &tree, false); err != nil {
		return err
	}
	if tree.Truncated || tree.SHA != sha || tree.Tree == nil {
		return fmt.Errorf("read tracked tree: incomplete subtree")
	}
	for _, entry := range tree.Tree {
		if entry.Path == "" || strings.Contains(entry.Path, "/") || entry.SHA == "" {
			return fmt.Errorf("read tracked tree: invalid subtree entry")
		}
		path := prefix + entry.Path
		switch entry.Type {
		case "tree":
			if err := c.walkTrackedTree(base, entry.SHA, path+"/", blobs, ancestors); err != nil {
				return err
			}
		case "blob":
			if blobs[path] != "" {
				return fmt.Errorf("read tracked tree: duplicate path")
			}
			blobs[path] = entry.SHA
		case "commit": // do not fetch objects from submodules
		default:
			return fmt.Errorf("read tracked tree: unknown object type")
		}
	}
	return nil
}

func (c *Client) readTrackedBlob(base, sha string) ([]byte, error) {
	var blob struct {
		SHA      string  `json:"sha"`
		Encoding string  `json:"encoding"`
		Content  *string `json:"content"`
		Size     *int    `json:"size"`
	}
	if _, err := c.safeTreeGet(base+"/git/blobs/"+url.PathEscape(sha), &blob, false); err != nil {
		return nil, err
	}
	if blob.SHA != sha || blob.Encoding != "base64" || blob.Content == nil || blob.Size == nil {
		return nil, fmt.Errorf("read tracked blob: identity or encoding mismatch")
	}
	body, err := base64.StdEncoding.Strict().DecodeString(*blob.Content)
	if err != nil || len(body) != *blob.Size {
		return nil, fmt.Errorf("read tracked blob: invalid or incomplete contents")
	}
	return body, nil
}
