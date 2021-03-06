package aws

// <!-- START clutchdoc -->
// description: Locates resources in the Amazon Web Services (AWS) cloud.
// <!-- END clutchdoc -->

import (
	"context"
	"errors"
	"fmt"

	"github.com/golang/protobuf/descriptor"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/any"
	"github.com/uber-go/tally"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	ec2v1api "github.com/lyft/clutch/backend/api/aws/ec2/v1"
	kinesisv1api "github.com/lyft/clutch/backend/api/aws/kinesis/v1"
	awsv1resolver "github.com/lyft/clutch/backend/api/resolver/aws/v1"
	resolverv1 "github.com/lyft/clutch/backend/api/resolver/v1"
	"github.com/lyft/clutch/backend/resolver"
	"github.com/lyft/clutch/backend/service"
	"github.com/lyft/clutch/backend/service/aws"
)

const Name = "clutch.resolver.aws"

// Output types (want).
var typeURLInstance = resolver.TypeURL((*ec2v1api.Instance)(nil))
var typeURLAutoscalingGroup = resolver.TypeURL((*ec2v1api.AutoscalingGroup)(nil))
var typeURLKinesisStream = resolver.TypeURL((*kinesisv1api.Stream)(nil))

var typeSchemas = map[string][]descriptor.Message{
	typeURLInstance: {
		(*awsv1resolver.InstanceID)(nil),
	},
	typeURLAutoscalingGroup: {
		(*awsv1resolver.AutoscalingGroupName)(nil),
	},
	typeURLKinesisStream: {
		(*awsv1resolver.KinesisStreamName)(nil),
	},
}

func makeRegionOptions(regions []string) []*resolverv1.Option {
	ret := make([]*resolverv1.Option, len(regions))
	for i, region := range regions {
		ret[i] = &resolverv1.Option{
			Value: &resolverv1.Option_StringValue{StringValue: region},
		}
	}
	return ret
}

func New(cfg *any.Any, logger *zap.Logger, scope tally.Scope) (resolver.Resolver, error) {
	awsClient, ok := service.Registry["clutch.service.aws"]
	if !ok {
		return nil, errors.New("could not find service")
	}

	c, ok := awsClient.(aws.Client)
	if !ok {
		return nil, errors.New("service was not the correct type")
	}

	schemas, err := resolver.InputsToSchemas(typeSchemas)
	if err != nil {
		return nil, err
	}

	resolver.HydrateDynamicOptions(schemas, map[string][]*resolverv1.Option{
		"regions": makeRegionOptions(c.Regions()),
	})

	r := &res{
		client:  c,
		schemas: schemas,
	}
	return r, nil
}

type res struct {
	client  aws.Client
	schemas resolver.TypeURLToSchemasMap
}

func (r *res) determineRegionsForOption(option string) []string {
	var regions []string
	switch option {
	case resolver.OptionAll:
		regions = r.client.Regions()
	default:
		regions = []string{option}
	}
	return regions
}

func (r *res) Schemas() resolver.TypeURLToSchemasMap { return r.schemas }

func (r *res) Resolve(ctx context.Context, wantTypeURL string, input proto.Message, limit uint32) (*resolver.Results, error) {
	switch wantTypeURL {
	case typeURLInstance:
		return r.resolveInstancesForInput(ctx, input)

	case typeURLAutoscalingGroup:
		return r.resolveAutoscalingGroupsForInput(ctx, input)

	case typeURLKinesisStream:
		return r.resolveKinesisStreamForInput(ctx, input)

	default:
		return nil, fmt.Errorf("don't know how to resolve type %s", wantTypeURL)
	}
}

func (r *res) Search(ctx context.Context, typeURL, query string, limit uint32) (*resolver.Results, error) {
	switch typeURL {
	case typeURLInstance:
		id, err := normalizeInstanceID(query)
		if err != nil {
			return nil, err
		}
		return r.instanceResults(ctx, resolver.OptionAll, []string{id}, limit)

	case typeURLAutoscalingGroup:
		return r.autoscalingGroupResults(ctx, resolver.OptionAll, []string{query}, limit)

	case typeURLKinesisStream:
		return r.kinesisResults(ctx, resolver.OptionAll, query, limit)

	default:
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("cannot search for type '%s'", typeURL))
	}
}
