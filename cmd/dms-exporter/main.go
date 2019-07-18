package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/external"
	"github.com/aws/aws-sdk-go-v2/service/databasemigrationservice"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

var (
	roleSessionName = "assumeTestRole"
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

// CredentialsProvider - Holder of credentials
type CredentialsProvider struct {
	*sts.Credentials
}

// Retrieve - Retriver of api keys
func (s CredentialsProvider) Retrieve() (aws.Credentials, error) {

	if s.Credentials == nil {
		return aws.Credentials{}, errors.New("sts credentials are nil")
	}

	return aws.Credentials{
		AccessKeyID:     aws.StringValue(s.AccessKeyId),
		SecretAccessKey: aws.StringValue(s.SecretAccessKey),
		SessionToken:    aws.StringValue(s.SessionToken),
		Expires:         aws.TimeValue(s.Expiration),
	}, nil
}

func getClient() aws.Config {
	cfg, err := external.LoadDefaultAWSConfig()
	// TODO: This will SEGFAULT, as error isn't fully handled
	if err != nil {
		logger.Error("unable to load SDK config, " + err.Error())
	}

	if *role != "" {
		// assume role
		svc := sts.New(cfg)
		input := &sts.AssumeRoleInput{RoleArn: aws.String(*role), RoleSessionName: aws.String(roleSessionName)}
		out, err := svc.AssumeRoleRequest(input).Send(context.Background())
		if err != nil {
			logger.Errorf("aws assume role %s: %v", *role, err)
		}

		awsConfig := svc.Config.Copy()
		awsConfig.Credentials = CredentialsProvider{Credentials: out.Credentials}
		cfg = awsConfig
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
	// TODO: Changing Logging to be structured and event based
	go func() {
		for {
			logger.Info("Chedcking Migration tasks. ", *role)
			getMigrationTasks()
			// TODO: Maybe make this tunable?
			time.Sleep(45 * time.Second)
		}
	}()

	go func() {
		for {
			logger.Info("Checking Migration instances. ", *role)
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
