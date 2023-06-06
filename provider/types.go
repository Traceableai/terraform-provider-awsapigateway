package provider

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	v1 "github.com/aws/aws-sdk-go-v2/service/apigateway"
	v2 "github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
)

type apiGatewayAction string

const (
	INCLUDE apiGatewayAction = "include"
	EXCLUDE apiGatewayAction = "exclude"
)

var (
	ApiGatewayActions     = []string{string(INCLUDE), string(EXCLUDE)}
	AccessLogFormatValues = []string{"$context.httpMethod", "$context.domainName", "$context.status", "$context.path"}
)

type Summary string

const (
	WrongSyntax                          Summary = "api gateway syntax is wrong"
	FullRequestAndResponseLogNotEnabled  Summary = "Full Request and Response Logs not enabled"
	ExecutionLogErrorOnly                Summary = "Execution Logs set to Errors Only"
	ExecutionLogNotEnabled               Summary = "Execution Logs not enabled"
	AccessLogNotEnabledREST              Summary = "REST API Access Logs not enabled"
	AccessLogNotEnabledHTTP              Summary = "HTTP API Access Logs not enabled"
	AccessLogFormatNotJson               Summary = "Access Log Format is not JSON parsable"
	AccessLogFormatMissingRequiredValues Summary = "Access Log Format is missing required values"
)

type MapDiagnostics struct {
	diagnostics      diag.Diagnostics
	warnDiagnostics  map[string][]string
	errorDiagnostics map[string][]string
}

type Option func(summary *string)

func WithMissingValues(values []string) Option {
	return func(summary *string) {
		*summary = fmt.Sprintf("%s %s", *summary, stringFromArray(values))
	}
}
func (s Summary) new(opts ...Option) string {
	summary := string(s)
	for _, opt := range opts {
		opt(&summary)
	}
	return summary
}

func (m *MapDiagnostics) add(diagnostic *diag.Diagnostic) {
	m.diagnostics = append(m.diagnostics, *diagnostic)
}
func (m *MapDiagnostics) addError(summary string, value string) {
	m.errorDiagnostics[summary] = append(m.errorDiagnostics[summary], value)
}
func (m *MapDiagnostics) addWarn(summary string, value string) {
	m.warnDiagnostics[summary] = append(m.warnDiagnostics[summary], value)
}
func (m *MapDiagnostics) getDiagnostics() diag.Diagnostics {
	diagnostics := m.diagnostics
	for summary, values := range m.warnDiagnostics {
		consolidatedSummary := fmt.Sprintf("%s for %s", summary, stringFromArray(values))
		diagnostics = append(diagnostics, *warnDiagnostic(consolidatedSummary))
	}
	for summary, values := range m.errorDiagnostics {
		consolidatedSummary := fmt.Sprintf("%s for %s", summary, stringFromArray(values))
		diagnostics = append(diagnostics, *errorDiagnostic(consolidatedSummary))
	}
	return diagnostics
}

///////////////////////////////////////////////////////////////////////////////
//                           apiGatewayProvider                              //
///////////////////////////////////////////////////////////////////////////////

type AwsApiGatewayProvider interface {
	getAwsGetRestApisPaginator() AwsGetRestApisPaginator
	getApiGatewayClient() AwsApiGatewayClient
	getApiGatewayV2Client() AwsApiGatewayV2Client
}

type apiGatewayProvider struct {
	config             aws.Config
	apiGatewayClient   AwsApiGatewayClient
	apiGatewayV2Client AwsApiGatewayV2Client
}

type AwsApiGatewayClient interface {
	GetRestApis(ctx context.Context, params *v1.GetRestApisInput, optFns ...func(*v1.Options)) (*v1.GetRestApisOutput, error)
	GetStages(ctx context.Context, params *v1.GetStagesInput, optFns ...func(*v1.Options)) (*v1.GetStagesOutput, error)
}

type AwsApiGatewayV2Client interface {
	GetApis(ctx context.Context, params *v2.GetApisInput, optFns ...func(*v2.Options)) (*v2.GetApisOutput, error)
	GetStages(ctx context.Context, params *v2.GetStagesInput, optFns ...func(*v2.Options)) (*v2.GetStagesOutput, error)
}

type AwsGetRestApisPaginator interface {
	HasMorePages() bool
	NextPage(ctx context.Context, optFns ...func(*v1.Options)) (*v1.GetRestApisOutput, error)
}

var _ AwsApiGatewayProvider = (*apiGatewayProvider)(nil)

func (p *apiGatewayProvider) getAwsGetRestApisPaginator() AwsGetRestApisPaginator {
	return v1.NewGetRestApisPaginator(p.apiGatewayClient, &v1.GetRestApisInput{})
}

func (p *apiGatewayProvider) getApiGatewayClient() AwsApiGatewayClient {
	return p.apiGatewayClient
}

func (p *apiGatewayProvider) getApiGatewayV2Client() AwsApiGatewayV2Client {
	return p.apiGatewayV2Client
}

func newFromConfig(cfg aws.Config) *apiGatewayProvider {
	return &apiGatewayProvider{
		config:             cfg,
		apiGatewayClient:   v1.NewFromConfig(cfg),
		apiGatewayV2Client: v2.NewFromConfig(cfg),
	}
}
