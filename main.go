/*
Main application package
*/
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	gitlab "github.com/xanzy/go-gitlab"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/urfave/cli"

	"github.com/Labbs/gitlab-ci-pipelines-exporter/settings"
)

var version = "v1.1"

var (
	timeSinceLastRun = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gitlab_ci_pipeline_time_since_last_run_seconds",
			Help: "Elapsed time since most recent GitLab CI pipeline run.",
		},
		[]string{"projectWithNamespace", "ref", "id"},
	)

	lastRunDuration = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gitlab_ci_pipeline_last_run_duration_seconds",
			Help: "Duration of last pipeline run",
		},
		[]string{"projectWithNamespace", "ref", "id"},
	)

	status = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gitlab_ci_pipeline_status",
			Help: "GitLab CI pipeline current status",
		},
		[]string{"projectWithNamespace", "ref", "status", "id"},
	)
)

// main init configuration
func main() {
	app := cli.NewApp()
	app.Name = "gitlab-ci-pipelines-exporter"
	app.Flags = settings.NewContext()
	app.Action = runWeb
	app.Version = version

	app.Run(os.Args)
}

// runWeb start http server
func runWeb(ctx *cli.Context) {
	go getGitlabInfo()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "/metrics")
	})
	http.Handle("/metrics", promhttp.Handler())
	log.Printf("starting exporter with port %v", settings.Port)
	log.Fatal(http.ListenAndServe(":"+strconv.Itoa(settings.Port), nil))
}

// init prometheus metrics
func init() {
	prometheus.MustRegister(timeSinceLastRun)
	prometheus.MustRegister(lastRunDuration)
	prometheus.MustRegister(status)
}

// getGitlabInfo get all needed informations from gitlab instance and update some metrics
func getGitlabInfo() {
	// init gitlab configuration
	client := gitlab.NewClient(nil, settings.Gitlab.Token)
	client.SetBaseURL(settings.Gitlab.Url)

	// get all projects
	opt := &gitlab.ListProjectsOptions{
		ListOptions: gitlab.ListOptions{
			PerPage: 50,
			Page:    1,
		},
		Owned: &settings.Gitlab.Owned,
	}

	for{
		projects, resp, err := client.Projects.ListProjects(opt)
		if err != nil {
			log.Fatalln(err)
		}

		// List all the projects we've found so far.
		for _, project := range projects {

		pipelines, _, _ := client.Pipelines.ListProjectPipelines(project.ID, &gitlab.ListProjectPipelinesOptions{})
		var lastPipeline *gitlab.Pipeline

		if len(pipelines) != 0 {

			lastPipeline, _, _ = client.Pipelines.GetPipeline(project.ID, pipelines[0].ID)
			lastRunDuration.WithLabelValues(project.NameWithNamespace, pipelines[0].Ref, strconv.Itoa(pipelines[0].ID)).Set(float64(lastPipeline.Duration))

			for _, s := range []string{"success", "failed", "running"} {
				if s == lastPipeline.Status {
					status.WithLabelValues(project.NameWithNamespace, pipelines[0].Ref, s, strconv.Itoa(pipelines[0].ID)).Set(1)
				} else {
					status.WithLabelValues(project.NameWithNamespace, pipelines[0].Ref, s, strconv.Itoa(pipelines[0].ID)).Set(0)
				}
			}

			timeSinceLastRun.WithLabelValues(
				project.NameWithNamespace,
				pipelines[0].Ref,
				strconv.Itoa(pipelines[0].ID)).Set(
				float64(time.Since(*lastPipeline.CreatedAt).Round(time.Second).Seconds()))
			}

		}

		// Exit the loop when we've seen all pages.
		if opt.Page >= resp.TotalPages {
			break
		}

		// Update the page number to get the next page.
		opt.Page = resp.NextPage

	}

	time.Sleep(time.Duration(settings.Gitlab.Refresh) * time.Second)
}
