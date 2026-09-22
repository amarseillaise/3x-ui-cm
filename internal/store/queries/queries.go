// Package queries holds every SQL statement the store runs. The statements
// live in .sql files next to this one, so they can be read, diffed and pasted
// into a sqlite3 shell without picking them out of Go string literals.
//
// Each statement is introduced by a "-- name: <key>" comment and bound to a
// field of Set at package initialisation. A key that no field claims, or a
// field no key fills, stops the program at startup rather than at the moment
// some rarely used query first runs.
package queries

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed *.sql
var files embed.FS

// Set is every statement, one field per "-- name:" key.
type Set struct {
	SessionInsert     string
	SessionGet        string
	SessionTouch      string
	SessionDelete     string
	SessionDeleteIdle string
	SessionCount      string

	PushUpsert            string
	PushDelete            string
	PushList              string
	PushListByRole        string
	PushListBySubIDs      string // holds a %s for the id placeholders
	PushListByRoleAndSubs string // holds a %s for the id placeholders
	PushMarkDelivered     string
	PushMarkFailed        string
	PushCountForSub       string
	PushCountByRole       string

	NotificationInsert      string
	NotificationCount       string
	NotificationCountForRef string

	RenewalInsert             string
	RenewalGet                string
	RenewalList               string
	RenewalListByStatus       string
	RenewalListBySub          string
	RenewalListPendingBefore  string
	RenewalCountPendingForSub string
	RenewalSetOutcome         string
	RenewalResolve            string
	RenewalCountByStatus      string

	MigrationCreateTable string
	MigrationListApplied string
	MigrationRecord      string
}

// Q is the loaded statement set.
var Q = mustLoad()

// bindings maps a "-- name:" key to the field that receives it.
func (s *Set) bindings() map[string]*string {
	return map[string]*string{
		"session.insert":      &s.SessionInsert,
		"session.get":         &s.SessionGet,
		"session.touch":       &s.SessionTouch,
		"session.delete":      &s.SessionDelete,
		"session.delete_idle": &s.SessionDeleteIdle,
		"session.count":       &s.SessionCount,

		"push.upsert":                   &s.PushUpsert,
		"push.delete":                   &s.PushDelete,
		"push.list":                     &s.PushList,
		"push.list_by_role":             &s.PushListByRole,
		"push.list_by_sub_ids":          &s.PushListBySubIDs,
		"push.list_by_role_and_sub_ids": &s.PushListByRoleAndSubs,
		"push.mark_delivered":           &s.PushMarkDelivered,
		"push.mark_failed":              &s.PushMarkFailed,
		"push.count_for_sub":            &s.PushCountForSub,
		"push.count_by_role":            &s.PushCountByRole,

		"notification.insert":        &s.NotificationInsert,
		"notification.count":         &s.NotificationCount,
		"notification.count_for_ref": &s.NotificationCountForRef,

		"renewal.insert":                &s.RenewalInsert,
		"renewal.get":                   &s.RenewalGet,
		"renewal.list":                  &s.RenewalList,
		"renewal.list_by_status":        &s.RenewalListByStatus,
		"renewal.list_by_sub":           &s.RenewalListBySub,
		"renewal.list_pending_before":   &s.RenewalListPendingBefore,
		"renewal.count_pending_for_sub": &s.RenewalCountPendingForSub,
		"renewal.set_outcome":           &s.RenewalSetOutcome,
		"renewal.resolve":               &s.RenewalResolve,
		"renewal.count_by_status":       &s.RenewalCountByStatus,

		"migration.create_table": &s.MigrationCreateTable,
		"migration.list_applied": &s.MigrationListApplied,
		"migration.record":       &s.MigrationRecord,
	}
}

func mustLoad() *Set {
	set, err := load()
	if err != nil {
		panic("store/queries: " + err.Error())
	}
	return set
}

func load() (*Set, error) {
	statements, err := parseAll()
	if err != nil {
		return nil, err
	}
	set := &Set{}
	bound := set.bindings()

	var missing []string
	for key, field := range bound {
		text, ok := statements[key]
		if !ok {
			missing = append(missing, key)
			continue
		}
		*field = text
	}
	var orphaned []string
	for key := range statements {
		if _, ok := bound[key]; !ok {
			orphaned = append(orphaned, key)
		}
	}
	sort.Strings(missing)
	sort.Strings(orphaned)
	switch {
	case len(missing) > 0 && len(orphaned) > 0:
		return nil, fmt.Errorf("no statement for %v, no field for %v", missing, orphaned)
	case len(missing) > 0:
		return nil, fmt.Errorf("no statement for %v", missing)
	case len(orphaned) > 0:
		return nil, fmt.Errorf("no field for %v", orphaned)
	}
	return set, nil
}

// parseAll reads every embedded .sql file into key -> statement.
func parseAll() (map[string]string, error) {
	names, err := fs.Glob(files, "*.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	out := map[string]string{}
	for _, name := range names {
		body, err := files.ReadFile(name)
		if err != nil {
			return nil, err
		}
		if err := parseFile(name, string(body), out); err != nil {
			return nil, err
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no statements found in %v", names)
	}
	return out, nil
}

const namePrefix = "-- name:"

// trimTrailingComments drops the comment block that introduces the next
// statement, so it is not swallowed by the statement above it. A comment
// between two statements documents the one below and is dropped; to keep a
// note with a statement, put it after that statement's "-- name:" line.
func trimTrailingComments(lines []string) []string {
	for len(lines) > 0 {
		last := strings.TrimSpace(lines[len(lines)-1])
		if last != "" && !strings.HasPrefix(last, "--") {
			break
		}
		lines = lines[:len(lines)-1]
	}
	return lines
}

func parseFile(file, body string, out map[string]string) error {
	var key string
	var lines []string
	flush := func() error {
		if key == "" {
			return nil
		}
		text := strings.TrimSpace(strings.Join(trimTrailingComments(lines), "\n"))
		if text == "" {
			return fmt.Errorf("%s: %q has no statement", file, key)
		}
		if _, exists := out[key]; exists {
			return fmt.Errorf("%s: %q is declared twice", file, key)
		}
		out[key] = strings.TrimSuffix(text, ";")
		return nil
	}
	for _, line := range strings.Split(body, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), namePrefix); ok {
			if err := flush(); err != nil {
				return err
			}
			key, lines = strings.TrimSpace(rest), nil
			if key == "" {
				return fmt.Errorf("%s: empty name", file)
			}
			continue
		}
		if key != "" {
			lines = append(lines, line)
		}
	}
	return flush()
}
