package cloudrungrader

import (
	"context"
	"fmt"
	"io"
	"time"

	run "cloud.google.com/go/run/apiv2"
	"cloud.google.com/go/run/apiv2/runpb"
	"cloud.google.com/go/storage"
	"google.golang.org/protobuf/types/known/durationpb"
)

// jobRunner starts one Cloud Run Job execution and waits for it to finish.
// A non-nil error means the execution failed or could not be started.
type jobRunner interface {
	Run(ctx context.Context, jobName string, env map[string]string) error
}

// objectStore is the slice of GCS the grader needs: direct object access for
// the app plus signed URLs handed to the (credential-less) runner job.
type objectStore interface {
	Put(ctx context.Context, key string, data []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
	SignedGetURL(key string, expiry time.Duration) (string, error)
	SignedPutURL(key, contentType string, expiry time.Duration) (string, error)
}

const jobTaskTimeout = 90 * time.Second

type runJobs struct {
	client  *run.JobsClient
	project string
	region  string
}

func (r *runJobs) Run(ctx context.Context, jobName string, env map[string]string) error {
	vars := make([]*runpb.EnvVar, 0, len(env))
	for k, v := range env {
		vars = append(vars, &runpb.EnvVar{Name: k, Values: &runpb.EnvVar_Value{Value: v}})
	}
	op, err := r.client.RunJob(ctx, &runpb.RunJobRequest{
		Name: fmt.Sprintf("projects/%s/locations/%s/jobs/%s", r.project, r.region, jobName),
		Overrides: &runpb.RunJobRequest_Overrides{
			ContainerOverrides: []*runpb.RunJobRequest_Overrides_ContainerOverride{{Env: vars}},
			TaskCount:          1,
			Timeout:            durationpb.New(jobTaskTimeout),
		},
	})
	if err != nil {
		return fmt.Errorf("start job %s: %w", jobName, err)
	}
	if _, err := op.Wait(ctx); err != nil {
		return fmt.Errorf("job %s execution: %w", jobName, err)
	}
	return nil
}

type gcsStore struct {
	bucket *storage.BucketHandle
}

func (g *gcsStore) Put(ctx context.Context, key string, data []byte) error {
	w := g.bucket.Object(key).NewWriter(ctx)
	if _, err := w.Write(data); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}

func (g *gcsStore) Get(ctx context.Context, key string) ([]byte, error) {
	r, err := g.bucket.Object(key).NewReader(ctx)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func (g *gcsStore) Delete(ctx context.Context, key string) error {
	return g.bucket.Object(key).Delete(ctx)
}

func (g *gcsStore) SignedGetURL(key string, expiry time.Duration) (string, error) {
	return g.bucket.SignedURL(key, &storage.SignedURLOptions{
		Scheme:  storage.SigningSchemeV4,
		Method:  "GET",
		Expires: time.Now().Add(expiry),
	})
}

func (g *gcsStore) SignedPutURL(key, contentType string, expiry time.Duration) (string, error) {
	return g.bucket.SignedURL(key, &storage.SignedURLOptions{
		Scheme:      storage.SigningSchemeV4,
		Method:      "PUT",
		ContentType: contentType,
		Expires:     time.Now().Add(expiry),
	})
}
