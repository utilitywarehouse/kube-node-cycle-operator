package meta

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
)

func bodyToString(body io.Reader) string {
	buf := new(bytes.Buffer)
	buf.ReadFrom(body)
	return buf.String()
}

func getMetaInstanceItem(item string) (string, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("http://metadata.google.internal/computeMetadata/v1/instance/%s", item), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Metadata-Flavor", "Google")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	return bodyToString(resp.Body), nil
}

func InstanceName() (string, error) {

	return getMetaInstanceItem("name")

}

func InstanceHostname() (string, error) {

	return getMetaInstanceItem("hostname")

}

func InstanceZone() (string, error) {

	return getMetaInstanceItem("zone")

}
