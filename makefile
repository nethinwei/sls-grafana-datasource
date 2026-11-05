darwin:
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o dist/gpx_sls-datasource_darwin_amd64 ./pkg
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o dist/gpx_sls-datasource_darwin_arm64 ./pkg
linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o dist/gpx_sls-datasource_linux_amd64 ./pkg
	CGO_ENABLED=0 GOOS=linux GOARCH=arm go build -o dist/gpx_sls-datasource_linux_arm ./pkg
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o dist/gpx_sls-datasource_linux_arm64 ./pkg
windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o dist/gpx_sls-datasource_windows_amd64.exe ./pkg
clean:
	rm -f dist/gpx_sls-datasource_*
all: clean darwin linux windows
#mage build