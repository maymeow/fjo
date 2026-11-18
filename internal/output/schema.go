package output

import (
	"fmt"
	"reflect"
	"strings"
	"time"
)

// The types in this file are the normalized JSON models exposed by --json
// (IMPLEMENTATION §10.2). They are deliberately independent of the Forgejo
// response structures: field names, types and nullability are a compatibility
// contract and change only with a schema version.

// User is the minimal user object used in JSON output.
type User struct {
	Login string `json:"login"`
}

// Label is a Forgejo label.
type Label struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// Milestone is a Forgejo milestone.
type Milestone struct {
	Title  string `json:"title"`
	Number int    `json:"number"`
}

// Repository is the normalized repository model used by `repo list --json`
// and `repo view --json`.
type Repository struct {
	Name             string        `json:"name"`
	NameWithOwner    string        `json:"nameWithOwner"`
	Description      string        `json:"description"`
	IsPrivate        bool          `json:"isPrivate"`
	IsInternal       bool          `json:"isInternal"`
	IsFork           bool          `json:"isFork"`
	IsMirror         bool          `json:"isMirror"`
	IsTemplate       bool          `json:"isTemplate"`
	IsArchived       bool          `json:"isArchived"`
	IsEmpty          bool          `json:"isEmpty"`
	DefaultBranch    string        `json:"defaultBranch"`
	PrimaryLanguage  string        `json:"primaryLanguage"`
	Topics           []string      `json:"topics"`
	URL              string        `json:"url"`
	CloneURL         string        `json:"cloneUrl"`
	SSHURL           string        `json:"sshUrl"`
	WebsiteURL       string        `json:"websiteUrl"`
	StargazerCount   int           `json:"stargazerCount"`
	ForkCount        int           `json:"forkCount"`
	OpenIssueCount   int           `json:"openIssueCount"`
	OpenPullRequests int           `json:"openPullRequestCount"`
	Parent           *RepositoryID `json:"parent"`
	CreatedAt        time.Time     `json:"createdAt"`
	UpdatedAt        time.Time     `json:"updatedAt"`
}

// RepositoryID identifies a related repository without recursing into the
// full repository model.
type RepositoryID struct {
	NameWithOwner string `json:"nameWithOwner"`
}

// Issue is the normalized issue model used by `issue ... --json`.
type Issue struct {
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	State     string     `json:"state"`
	Author    User       `json:"author"`
	URL       string     `json:"url"`
	Body      string     `json:"body"`
	Labels    []Label    `json:"labels"`
	Milestone *Milestone `json:"milestone"`
	Assignees []User     `json:"assignees"`
	Comments  int        `json:"comments"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	ClosedAt  *time.Time `json:"closedAt"`
}

// PullRequest is the normalized pull request model used by `pr ... --json`.
type PullRequest struct {
	Number         int        `json:"number"`
	Title          string     `json:"title"`
	State          string     `json:"state"`
	IsDraft        bool       `json:"isDraft"`
	Author         User       `json:"author"`
	URL            string     `json:"url"`
	Body           string     `json:"body"`
	BaseRefName    string     `json:"baseRefName"`
	HeadRefName    string     `json:"headRefName"`
	HeadRepository string     `json:"headRepository"`
	IsCrossRepo    bool       `json:"isCrossRepository"`
	Mergeable      string     `json:"mergeable"`
	Labels         []Label    `json:"labels"`
	Milestone      *Milestone `json:"milestone"`
	Assignees      []User     `json:"assignees"`
	Reviewers      []User     `json:"reviewers"`
	Comments       int        `json:"comments"`
	Additions      int        `json:"additions"`
	Deletions      int        `json:"deletions"`
	ChangedFiles   int        `json:"changedFiles"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	ClosedAt       *time.Time `json:"closedAt"`
	MergedAt       *time.Time `json:"mergedAt"`
}

// Comment is the normalized issue/PR comment model.
type Comment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	Author    User      `json:"author"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Review is the normalized pull request review model.
type Review struct {
	ID          int64      `json:"id"`
	State       string     `json:"state"`
	Body        string     `json:"body"`
	Author      User       `json:"author"`
	URL         string     `json:"url"`
	SubmittedAt *time.Time `json:"submittedAt"`
}

// Check is one commit status check for `pr checks --json`.
type Check struct {
	Name        string     `json:"name"`
	State       string     `json:"state"`
	Bucket      string     `json:"bucket"`
	Description string     `json:"description"`
	URL         string     `json:"url"`
	StartedAt   *time.Time `json:"startedAt"`
	CompletedAt *time.Time `json:"completedAt"`
}

// RepositoryFields lists the selectable fields for repository JSON output.
var RepositoryFields = []string{
	"cloneUrl", "createdAt", "defaultBranch", "description", "forkCount",
	"isArchived", "isEmpty", "isFork", "isInternal", "isMirror", "isPrivate",
	"isTemplate", "name", "nameWithOwner", "openIssueCount",
	"openPullRequestCount", "parent", "primaryLanguage", "sshUrl",
	"stargazerCount", "topics", "updatedAt", "url", "websiteUrl",
}

// IssueFields lists the selectable fields for issue JSON output.
var IssueFields = []string{
	"assignees", "author", "body", "closedAt", "comments", "createdAt",
	"labels", "milestone", "number", "state", "title", "updatedAt", "url",
}

// PullRequestFields lists the selectable fields for pull request JSON output.
var PullRequestFields = []string{
	"additions", "assignees", "author", "baseRefName", "body", "changedFiles",
	"closedAt", "comments", "createdAt", "deletions", "headRefName",
	"headRepository", "isCrossRepository", "isDraft", "labels", "mergeable",
	"mergedAt", "milestone", "number", "reviewers", "state", "title",
	"updatedAt", "url",
}

// ExportData selects fields from v (a struct or a slice of structs) and
// returns JSON-ready data. Field lookup accepts the JSON name or the Go field
// name, case-insensitively, like gh.
func ExportData(v any, fields []string) (any, error) {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice:
		out := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			item, err := exportStruct(rv.Index(i), fields)
			if err != nil {
				return nil, err
			}
			out[i] = item
		}
		return out, nil
	default:
		return exportStruct(rv, fields)
	}
}

func exportStruct(v reflect.Value, fields []string) (map[string]any, error) {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil, fmt.Errorf("cannot export fields from nil")
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("cannot export fields from %s", v.Kind())
	}
	t := v.Type()
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		name, sf, ok := fieldByJSONName(t, f)
		if !ok {
			return nil, fmt.Errorf("unknown JSON field %q", f)
		}
		out[name] = v.Field(sf).Interface()
	}
	return out, nil
}

func fieldByJSONName(t reflect.Type, name string) (string, int, bool) {
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if sf.PkgPath != "" {
			continue
		}
		jsonName := strings.Split(sf.Tag.Get("json"), ",")[0]
		if jsonName == "-" {
			continue
		}
		if jsonName == "" {
			jsonName = sf.Name
		}
		if strings.EqualFold(jsonName, name) || strings.EqualFold(sf.Name, name) {
			return jsonName, i, true
		}
	}
	return "", 0, false
}

// TimeAgo renders a human-readable age like gh's `timeago` helper.
func TimeAgo(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "less than a minute ago"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute") + " ago"
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "hour") + " ago"
	case d < 30*24*time.Hour:
		return plural(int(d.Hours()/24), "day") + " ago"
	}
	return t.Format("Jan 2, 2006")
}

func plural(n int, unit string) string {
	if n == 1 {
		return "about 1 " + unit
	}
	return fmt.Sprintf("about %d %ss", n, unit)
}

// CheckFields lists the selectable fields for commit status output.
var CheckFields = []string{"bucket", "completedAt", "description", "name", "startedAt", "state", "url"}
