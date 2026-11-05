package main

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	sls "github.com/aliyun/aliyun-log-go-sdk"
)

// The field catalog is the single description of the fields a user sees for a logstore: SLS index
// information combined with what the field rules do. Completion, the field browser, query
// diagnostics, field filters and dashboard filters all read it, so they agree with each other and
// with the rules, and none of them repeats the rules' logic.

type catalogField struct {
	Name string `json:"name"`           // name shown in Grafana
	Type string `json:"type,omitempty"` // text, long, double, json (from the index)
	// Indexed reports whether `field: value` search works on the field; nil when unknown (the
	// AccessKey cannot read the index).
	Indexed *bool `json:"indexed,omitempty"`
	// Stats reports whether SQL statistics are enabled (SQL, group by, value suggestions).
	Stats *bool `json:"stats,omitempty"`
	// SLSName is the field to write in SLS statements; empty for fields produced by rules, which
	// only exist in Grafana and cannot be filtered on.
	SLSName string `json:"slsName,omitempty"`
	// Rule is the rule type that produced or renamed the field.
	Rule string `json:"rule,omitempty"`
}

type catalog struct {
	Fields []catalogField `json:"fields"`
	// IndexKnown is false when the index could not be read; fields then come from sample logs and
	// Indexed / Stats are unknown.
	IndexKnown bool `json:"indexKnown"`
}

// byName returns the field shown under name.
func (c *catalog) byName(name string) (catalogField, bool) {
	for _, f := range c.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return catalogField{}, false
}

// catalogBuilder applies the rules' describe to the base fields.
type catalogBuilder struct {
	timeField string
	fields    map[string]*catalogField
}

func newCatalogBuilder(timeField string) *catalogBuilder {
	return &catalogBuilder{timeField: timeField, fields: map[string]*catalogField{}}
}

func (c *catalogBuilder) rename(from, to string) {
	src := c.fields[from]
	delete(c.fields, from)
	if _, own := c.fields[to]; own {
		return // the log's own field under the target name wins, as in renameRule.apply
	}
	f := catalogField{Name: to, SLSName: from, Rule: "rename"}
	if src != nil {
		f.Type, f.Indexed, f.Stats, f.SLSName = src.Type, src.Indexed, src.Stats, src.SLSName
	}
	c.fields[to] = &f
}

func (c *catalogBuilder) remove(name string) {
	delete(c.fields, name)
}

func (c *catalogBuilder) derive(name, ruleType string) {
	c.fields[name] = &catalogField{Name: name, Rule: ruleType}
}

func (c *catalogBuilder) result(indexKnown bool) *catalog {
	out := &catalog{IndexKnown: indexKnown, Fields: make([]catalogField, 0, len(c.fields))}
	for _, f := range c.fields {
		out.Fields = append(out.Fields, *f)
	}
	sort.Slice(out.Fields, func(a, b int) bool { return out.Fields[a].Name < out.Fields[b].Name })
	return out
}

// buildCatalog combines index keys (nil when the index cannot be read) with keys seen in sample
// logs, then applies the rules.
func buildCatalog(index *sls.Index, sample []map[string]string, view *logsView) *catalog {
	c := newCatalogBuilder(view.timeField)
	yes, no := true, false
	if index != nil {
		for name, k := range index.Keys {
			stats := k.DocValue
			c.fields[name] = &catalogField{Name: name, Type: k.Type, Indexed: &yes, Stats: &stats, SLSName: name}
			for sub, jk := range k.JsonKeys {
				if jk != nil {
					stats := jk.DocValue
					n := name + "." + sub
					c.fields[n] = &catalogField{Name: n, Type: jk.Type, Indexed: &yes, Stats: &stats, SLSName: n}
				}
			}
		}
	}
	for _, l := range sample {
		for name := range l {
			if _, ok := c.fields[name]; ok {
				continue
			}
			f := &catalogField{Name: name, SLSName: name}
			if index != nil {
				// SLS meta fields (__tag__:*, __source__, __topic__) are searchable without a field
				// index; other fields missing from the index are not.
				if strings.HasPrefix(name, "__") {
					f.Indexed = &yes
				} else {
					f.Indexed, f.Stats = &no, &no
				}
			}
			c.fields[name] = f
		}
	}
	for _, r := range view.rules {
		r.describe(c)
	}
	return c.result(index != nil)
}

// ---------- cache ----------

const catalogTTL = 5 * time.Minute

type catalogCache struct {
	mu      sync.Mutex
	entries map[string]catalogEntry
}

func (c *catalogCache) forget(logstore string) {
	c.mu.Lock()
	delete(c.entries, logstore)
	c.mu.Unlock()
}

type catalogEntry struct {
	at time.Time
	c  *catalog
}

// catalog returns the field catalog of a logstore, from cache when fresh. Reading the index and a
// sample of the last 15 minutes takes two SLS requests, made at most once per TTL and logstore.
func (ds *datasource) catalog(ctx context.Context, logstore string) (*catalog, error) {
	ds.catalogs.mu.Lock()
	e, ok := ds.catalogs.entries[logstore]
	ds.catalogs.mu.Unlock()
	if ok && time.Since(e.at) < catalogTTL {
		return e.c, nil
	}

	index, indexErr := call(ctx, func() (*sls.Index, error) { return ds.client.GetIndex(ds.cfg.Project, logstore) })
	sample, sampleErr := ds.sample(ctx, logstore, "*", 0, 0)
	if indexErr != nil && sampleErr != nil {
		return nil, indexErr
	}
	if indexErr != nil {
		index = nil
	}
	c := buildCatalog(index, sample, &ds.cfg.view)

	ds.catalogs.mu.Lock()
	if ds.catalogs.entries == nil {
		ds.catalogs.entries = map[string]catalogEntry{}
	}
	ds.catalogs.entries[logstore] = catalogEntry{time.Now(), c}
	ds.catalogs.mu.Unlock()
	return c, nil
}
