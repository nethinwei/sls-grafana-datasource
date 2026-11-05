package main

import (
	"os"
	_ "time/tzdata" // the SQL time zone setting must not depend on zoneinfo in the Grafana image

	sdkds "github.com/grafana/grafana-plugin-sdk-go/backend/datasource"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
)

const pluginID = "aliyun-log-service-datasource"

func main() {
	if err := sdkds.Manage(pluginID, newDatasource, sdkds.ManageOpts{}); err != nil {
		log.DefaultLogger.Error(err.Error())
		os.Exit(1)
	}
}
