package main

import (
	"encoding/json"
	"testing"
	"time"

	sls "github.com/aliyun/aliyun-log-go-sdk"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

func TestIsSQLIgnoresQuotedPipes(t *testing.T) {
	cases := map[string]bool{
		`msg: "a | select b"`:                        false,
		`* | select count(*)`:                        true,
		`* | set session mode=scan; select count(*)`: true,
		`* | where a > 1`:                            false,
		`msg: it's | select count(*) as c`:           true, // a single quote is an ordinary character in search
		`msg: "it's | not sql" and level: ERROR`:     false,
	}
	for q, want := range cases {
		if got := isSQL(q); got != want {
			t.Errorf("isSQL(%q) = %v", q, got)
		}
		if want && withPackMeta(q) != q {
			t.Errorf("withPackMeta must leave SQL alone: %q", q)
		}
	}
}

func TestFilterDetails(t *testing.T) {
	v := testViewLevel("severity")
	q, _ := applyAdhocFilters("*", []adhocFilter{
		{Key: "detected_level", Operator: "=", Value: "ERROR"},
		{Key: "user agent", Operator: "=", Value: "x"},
		{Key: "cost", Operator: ">", Value: "Inf"},
	}, v, nil, nil)
	if q != `severity: "ERROR" and "user agent": "x"` {
		t.Errorf("q = %q", q)
	}
}

func TestRulesKeepIndexedFields(t *testing.T) {
	index := &sls.Index{Keys: map[string]sls.IndexKey{"env": {Type: "text", DocValue: true}}}
	view := testView(append(append([]fieldRule{}, defaultFieldRules...), fieldRule{Type: "default", To: "env", Value: "prod"}), nil, true)
	c := buildCatalog(index, nil, view)
	if f, ok := c.byName("env"); !ok || f.SLSName != "env" {
		t.Errorf("env = %+v: a default for an indexed field keeps it filterable", f)
	}
}

func TestEndpointDefaultsToHTTPS(t *testing.T) {
	jsonData, _ := json.Marshal(map[string]string{"project": "p"})
	cfg, err := loadSettings(backend.DataSourceInstanceSettings{URL: "cn-hangzhou.log.aliyuncs.com", JSONData: jsonData,
		DecryptedSecureJSONData: map[string]string{"accessKeyId": "a", "accessKeySecret": "b"}})
	if err != nil || cfg.Endpoint != "https://cn-hangzhou.log.aliyuncs.com" {
		t.Errorf("%v %q", err, cfg.Endpoint)
	}
}

func TestAuthSettings(t *testing.T) {
	load := func(js map[string]string, secure map[string]string) (*settings, error) {
		jsonData, _ := json.Marshal(js)
		return loadSettings(backend.DataSourceInstanceSettings{URL: "cn-hangzhou.log.aliyuncs.com", JSONData: jsonData, DecryptedSecureJSONData: secure})
	}
	ak := map[string]string{"accessKeyId": "a", "accessKeySecret": "b"}
	if _, err := load(map[string]string{"project": "p"}, nil); err == nil {
		t.Error("the default authentication needs an AccessKey")
	}
	if cfg, err := load(map[string]string{"project": "p", "authType": "accessKey"}, ak); err != nil {
		t.Error(err)
	} else if _, ok := cfg.credentials().(*sls.StaticCredentialsProvider); !ok {
		t.Errorf("accessKey signs with the AccessKey: %T", cfg.credentials())
	}
	if _, err := load(map[string]string{"project": "p", "authType": "ecsRamRole", "ramRoleName": " "}, nil); err == nil {
		t.Error("ecsRamRole needs a role name")
	}
	cfg, err := load(map[string]string{"project": "p", "authType": "ecsRamRole", "ramRoleName": " role-a "}, nil)
	if err != nil || cfg.RAMRoleName != "role-a" {
		t.Fatalf("ecsRamRole needs no AccessKey: %v %+v", err, cfg)
	}
	if _, ok := cfg.credentials().(*sls.StaticCredentialsProvider); ok {
		t.Error("ecsRamRole must not sign with a static AccessKey")
	}
	if _, err := load(map[string]string{"project": "p", "authType": "sts"}, ak); err == nil {
		t.Error("an unknown authentication type is an error")
	}
}

func TestContextKeepsOrder(t *testing.T) {
	sec := time.Unix(1700000000, 0)
	f := data.NewFrame("logs", data.NewField("time", nil, []time.Time{sec, sec, sec}))
	keepOrder(f, false)
	if !f.Fields[0].At(1).(time.Time).Before(sec) || !f.Fields[0].At(2).(time.Time).Before(f.Fields[0].At(1).(time.Time)) {
		t.Errorf("backward rows must go back in time: %v", f.Fields[0])
	}
}

func TestEmptySQLTimeSeries(t *testing.T) {
	f, err := buildSQLFrame([]string{"time", "host", "c"}, []string{"long", "varchar", "long"}, nil, "time", time.UTC, 0)
	if err != nil {
		t.Fatalf("no rows is no data, not an error: %v", err)
	}
	if n, _ := f.RowLen(); n != 0 {
		t.Errorf("rows = %d", n)
	}
}
