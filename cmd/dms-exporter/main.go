package main

import (
	"context"
	"flag"
	//"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/aws/aws-sdk-go-v2/aws"
	//"github.com/aws/aws-sdk-go-v2/aws/endpoints"
	"github.com/aws/aws-sdk-go-v2/aws/external"
	"github.com/aws/aws-sdk-go-v2/aws/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/databasemigrationservice"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

var addr = flag.String("listen-address", ":8080", "The address to listen on for HTTP requests.")
var region = flag.String("region", "us-west-2", "AWS Region to use")
var role = flag.String("role", "", "AWS Role ARN to Assume if required")
var logger = logrus.New()

var (
	migrationTasksUp = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "aws",
		Subsystem: "database_migration_service",
		Name:      "migration_task_up",
		Help:      "AWS Database Migration Task Status",
	},
		[]string{
			"id",
		},
	)

	migrationInstancesUp = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "aws",
		Subsystem: "database_migration_service",
		Name:      "migration_instance_up",
		Help:      "AWS Database Migration Instance Status",
	},
		[]string{
			"id",
		},
	)
)

func getClient() aws.Config {
	cfg, err := external.LoadDefaultAWSConfig()
	// TODO: This will SEGFAULT, as error isn't fully handled
	if err != nil {
		logger.Error("unable to load SDK config, " + err.Error())
	}

	if *role != "" {
		// Create the credentials from AssumeRoleProvider to assume the role
		// referenced by the "myRoleARN" ARN.
		stsSvc := sts.New(cfg)
		stsCredProvider := stscreds.NewAssumeRoleProvider(stsSvc, *role)

		cfg.Credentials = stsCredProvider
	}

	// Set AWS Region
	cfg.Region = *region

	return cfg
}

func getMigrationTasks() {
	ctx := context.Context(context.Background())
	cfg := getClient()
	svc := databasemigrationservice.New(cfg)

	input := &databasemigrationservice.DescribeReplicationTasksInput{}

	req := svc.DescribeReplicationTasksRequest(input)

	result, err := req.Send(ctx)
	if err != nil {
		logger.Error("Error gathering migration tasks")
		return
	}

	for _, tsk := range result.ReplicationTasks {
		if *tsk.Status != "running" {
			migrationTasksUp.WithLabelValues(
				*tsk.ReplicationTaskIdentifier,
			).Set(0)
		} else {
			migrationTasksUp.WithLabelValues(
				*tsk.ReplicationTaskIdentifier,
			).Set(1)
		}
	}
}

func getMigrationInstances() {
	ctx := context.Context(context.Background())
	cfg := getClient()
	svc := databasemigrationservice.New(cfg)

	input := &databasemigrationservice.DescribeReplicationInstancesInput{}

	req := svc.DescribeReplicationInstancesRequest(input)

	result, err := req.Send(ctx)
	if err != nil {
		logger.Error("Error gathering Migration Instances")
		return
	}

	for _, tsk := range result.ReplicationInstances {
		if *tsk.ReplicationInstanceStatus != "available" {
			migrationInstancesUp.WithLabelValues(
				*tsk.ReplicationInstanceIdentifier,
			).Set(0)
		} else {
			migrationInstancesUp.WithLabelValues(
				*tsk.ReplicationInstanceIdentifier,
			).Set(1)
		}
	}
}

func tasks() {
	go func() {
		for {
			getMigrationTasks()
			// TODO: Maybe make this tunable?
			time.Sleep(45 * time.Second)
		}
	}()

	go func() {
		for {
			getMigrationInstances()
			time.Sleep(45 * time.Second)
		}
	}()
}

func main() {
	logger.Formatter = &logrus.TextFormatter{
		// disable, as we set our own
		FullTimestamp: true,
	}

	logger.Info("Starting Up....")

	flag.Parse()

	tasks()

	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
             <head><title>AWS DMS Exporter</title></head>
             <body>
             <h1>AWS Database Migration Service Exporter</h1>
             <p><a href='/metrics'>Metrics</a></p>
             </body>
             </html>`))
	})
	logger.Info("Starting up Webserver on port: ", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
	os.Exit(0)
}
