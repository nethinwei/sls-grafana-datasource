package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

// settings is the data source configuration: jsonData plus decrypted secureJsonData.
type settings struct {
	Endpoint      string // data source URL, e.g. cn-hangzhou.log.aliyuncs.com
	Project       string `json:"project"`
	LogStore      string `json:"logstore"`
	Region        string `json:"region"`        // set to sign requests with AuthV4
	International bool   `json:"international"` // console on alibabacloud.com instead of aliyun.com
	// SQLTimeZone is the zone of date-time strings returned by SLS SQL (timestamp columns,
	// from_unixtime, date_format), which carry no offset. SLS uses UTC+8 by default.
	SQLTimeZone     string `json:"sqlTimeZone"`
	sqlLocation     *time.Location
	AccessKeyID     string
	AccessKeySecret string
}

func loadSettings(s backend.DataSourceInstanceSettings) (*settings, error) {
	cfg := &settings{}
	if len(s.JSONData) > 0 {
		if err := json.Unmarshal(s.JSONData, cfg); err != nil {
			return nil, fmt.Errorf("read data source settings: %w", err)
		}
	}
	cfg.Endpoint = strings.TrimSpace(s.URL)
	cfg.AccessKeyID = s.DecryptedSecureJSONData["accessKeyId"]
	cfg.AccessKeySecret = s.DecryptedSecureJSONData["accessKeySecret"]

	if cfg.SQLTimeZone == "" {
		cfg.SQLTimeZone = "Asia/Shanghai"
	}
	loc, err := time.LoadLocation(cfg.SQLTimeZone)
	if err != nil {
		return nil, fmt.Errorf("invalid SQL time zone %q: %w", cfg.SQLTimeZone, err)
	}
	cfg.sqlLocation = loc

	switch {
	case cfg.Endpoint == "":
		return nil, errors.New("endpoint (URL) is not configured")
	case cfg.Project == "":
		return nil, errors.New("project is not configured")
	case cfg.AccessKeyID == "" || cfg.AccessKeySecret == "":
		return nil, errors.New("AccessKey is not configured")
	}
	return cfg, nil
}

// consoleDomain is the second-level domain of the SLS console for this account.
func (s *settings) consoleDomain() string {
	if s.International {
		return "alibabacloud"
	}
	return "aliyun"
}
