package helpers

import (
	"crypto/tls"
	"net"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/concourse/atc"
	"github.com/concourse/go-concourse/concourse"

	. "github.com/onsi/gomega"
)

const (
	pipelineName = "create-bosh-cloudfoundry"
	jobName      = "cf-deploy"
	resourceName = "pipeline-trigger"
)

type byStartTime []atc.Build

func (a byStartTime) Len() int           { return len(a) }
func (a byStartTime) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byStartTime) Less(i, j int) bool { return a[i].StartTime > a[j].StartTime }

type basicAuthTransport struct {
	username string
	password string
	base     http.RoundTripper
}

func getConfigFromEnvironment(varName string) string {
	configValue := os.Getenv(varName)
	Expect(configValue).NotTo(BeEmpty(), "Environment variable $%s is not set", varName)
	return configValue
}

func GetAppsDomainZoneName() string {
	return getConfigFromEnvironment("APPS_DNS_ZONE_NAME")
}

func GetResourceVersion() string {
	return getConfigFromEnvironment("PIPELINE_TRIGGER_VERSION")
}

func getConcourseAtcUrl() string {
	return getConfigFromEnvironment("CONCOURSE_ATC_URL")
}

func getConcourseUserName() string {
	return getConfigFromEnvironment("CONCOURSE_ATC_USERNAME")
}

func getConcoursePassword() string {
	return getConfigFromEnvironment("CONCOURSE_ATC_PASSWORD")
}

func getSkipSSLValidation() bool {
	return getConfigFromEnvironment("SKIP_SSL_VALIDATION") == "true"
}

func filterBuildsByNameAndSortByTime(builds []atc.Build, jobName string) []atc.Build {
	var filteredBuilds []atc.Build
	for _, build := range builds {
		if build.JobName == jobName {
			filteredBuilds = append(filteredBuilds, build)
		}
	}
	sort.Sort(byStartTime(filteredBuilds))
	return filteredBuilds
}

func (t basicAuthTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r.SetBasicAuth(t.username, t.password)
	return t.base.RoundTrip(r)
}

func newConcourseClient(atcUrl, username, password string) concourse.Client {
	var transport http.RoundTripper

	var tlsConfig *tls.Config
	tlsConfig = &tls.Config{InsecureSkipVerify: getSkipSSLValidation()}

	transport = &http.Transport{
		TLSClientConfig: tlsConfig,
		Dial: (&net.Dialer{
			Timeout: 10 * time.Second,
		}).Dial,
		Proxy: http.ProxyFromEnvironment,
	}

	client := concourse.NewClient(
		atcUrl,
		&http.Client{
			Transport: basicAuthTransport{
				username: username,
				password: password,
				base:     transport,
			},
		},
	)
	return client
}

func buildsWithVersion(client concourse.Client, pipelineName, resourceName, resourceVersion string) []atc.Build {
	var resourceVersionID int

	page := concourse.Page{
		Since: 0,
		Until: 0,
		Limit: 10,
	}

	resourceVersions, _, resourceExists, err := client.ResourceVersions(pipelineName, resourceName, page)
	Expect(err).NotTo(HaveOccurred())
	Expect(resourceExists).To(BeTrue())

	for _, version := range resourceVersions {
		if resourceVersion == version.Version["number"] {
			resourceVersionID = version.ID
		}
	}
	Expect(resourceVersionID).NotTo(Equal(0), "Resource: %s with version: %s should exist in Concourse", resourceName, resourceVersion)

	builds, _, err := client.BuildsWithVersionAsInput(pipelineName, resourceName, resourceVersionID)

	return filterBuildsByNameAndSortByTime(builds, jobName)
}

func DeploymentHasFinishedInConcourse() bool {
	client := newConcourseClient(getConcourseAtcUrl(), getConcourseUserName(), getConcoursePassword())
	builds := buildsWithVersion(client, pipelineName, resourceName, GetResourceVersion())
	if len(builds) != 0 && (builds[0].Status == "succeeded" || builds[0].Status == "failed") {
		return true
	}
	return false
}
