package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	v1 "github.com/aws/aws-sdk-go-v2/service/apigateway"
	v2 "github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

func AwsApiGatewayResource() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceCreateUpdate,
		ReadContext:   resourceRead,
		UpdateContext: resourceCreateUpdate,
		DeleteContext: resourceDelete,

		Schema: map[string]*schema.Schema{
			"api_gateways": {
				Type:     schema.TypeList,
				Required: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
			},
			"action": {
				Type:         schema.TypeString,
				Optional:     true,
				Default:      string(INCLUDE),
				ValidateFunc: validation.StringInSlice(ApiGatewayActions, false),
			},
			"identifier": {
				Type:     schema.TypeString,
				Optional: true,
				Default:  "",
			},
			"ignore_access_log_settings": {
				Type:     schema.TypeBool,
				Optional: true,
				Default:  false,
			},
			"log_group_names": {
				Type:     schema.TypeList,
				Computed: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
			},
		},
	}
}

func resourceCreateUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	apiGateways := d.Get("api_gateways").([]interface{})
	action := d.Get("action").(string)
	ignoreAccessLogSettings := d.Get("ignore_access_log_settings").(bool)
	mapDiagnostics := &MapDiagnostics{
		diagnostics:      diag.Diagnostics{},
		warnDiagnostics:  make(map[string][]string),
		errorDiagnostics: make(map[string][]string),
	}
	logGroupNames := getLogGroupNames(apiGateways, action == string(EXCLUDE), ignoreAccessLogSettings, meta, mapDiagnostics)
	if d.Id() == "" {
		d.SetId(uuid.New().String())
	}
	if err := d.Set("log_group_names", logGroupNames); err != nil {
		mapDiagnostics.add(errorDiagnostic(err.Error()))
	}
	return mapDiagnostics.getDiagnostics()
}

func resourceRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	return nil
}

func resourceDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	d.SetId("")
	return nil
}

func getLogGroupNames(apiGateways []interface{}, exclude bool, ignoreAccessLogSettings bool, meta interface{}, mapDiagnostics *MapDiagnostics) []string {
	var summary string
	if !exclude && len(apiGateways) == 0 {
		summary = "api_gateways cannot be empty when action is include."
		mapDiagnostics.add(errorDiagnostic(summary))
		return []string{}
	}

	// apiAllStages stores api ids where all stages need to be considered
	// apiWithStage is a map of api id to list of api stages that need to be considered
	// any api id can only belong to either apiAllStages slice or apiWithStage map
	var apiAllStages []string
	apiWithStage := make(map[string][]string)
	for _, elem := range apiGateways {
		value := elem.(string)
		apiDetails := strings.Split(value, "/")
		if len(apiDetails) == 2 {
			if !contains(apiAllStages, apiDetails[0]) {
				apiWithStage[apiDetails[0]] = append(apiWithStage[apiDetails[0]], apiDetails[1])
			}
		} else if len(apiDetails) == 1 {
			apiAllStages = append(apiAllStages, apiDetails[0])
			if _, ok := apiWithStage[apiDetails[0]]; ok {
				delete(apiWithStage, apiDetails[0])
			}
		} else {
			mapDiagnostics.addError(WrongSyntax.new(), value)
		}
	}

	conn := meta.(AwsApiGatewayProvider)
	logGroupNames := getLogGroupNamesRestApis(conn, apiAllStages, apiWithStage, exclude, ignoreAccessLogSettings, mapDiagnostics)
	apiGatewayV2LogGroupNames := getLogGroupNamesHttpApis(conn, apiAllStages, apiWithStage, exclude, ignoreAccessLogSettings, mapDiagnostics)
	return removeDuplicates(append(logGroupNames, apiGatewayV2LogGroupNames...))
}

func getLogGroupNamesRestApis(conn AwsApiGatewayProvider, apiAllStages []string, apiWithStage map[string][]string, exclude bool, ignoreAccessLogSettings bool, mapDiagnostics *MapDiagnostics) []string {
	// apiStageMappingRest is a map of api id to list of api stages that need to be considered
	// if the value list is empty, it means that all stages in this api should be considered
	apiStageMappingRest := make(map[string][]string)
	restApisPaginator := conn.getAwsGetRestApisPaginator()
	for restApisPaginator.HasMorePages() {
		res, err := restApisPaginator.NextPage(context.TODO())
		if err != nil {
			summary := fmt.Sprintf("Error while invoking getRestApis sdk call: %s", err.Error())
			mapDiagnostics.add(errorDiagnostic(summary))
			continue
		}
		for _, restApi := range res.Items {
			apiId := *restApi.Id
			apiStages, partial := apiWithStage[apiId]
			if partial {
				apiStageMappingRest[apiId] = apiStages
			} else if contains(apiAllStages, apiId) != exclude {
				apiStageMappingRest[apiId] = []string{}
			}
		}
	}
	logGroupNames := getLogGroupNamesRestApisHelper(conn, apiStageMappingRest, exclude, ignoreAccessLogSettings, mapDiagnostics)
	return logGroupNames
}

func getLogGroupNamesHttpApis(conn AwsApiGatewayProvider, apiAllStages []string, apiWithStage map[string][]string, exclude bool, ignoreAccessLogSettings bool, mapDiagnostics *MapDiagnostics) []string {
	var summary string
	// apiStageMappingRest is a map of api id to list of api stages that need to be considered
	// if the value list is empty, it means that all stages in this api should be considered
	apiStageMappingV2 := make(map[string][]string)
	apiGatewayV2Client := conn.getApiGatewayV2Client()
	res, err := apiGatewayV2Client.GetApis(context.TODO(), &v2.GetApisInput{})
	if err != nil {
		summary = fmt.Sprintf("Error while invoking getApis sdk call: %s", err.Error())
		mapDiagnostics.add(errorDiagnostic(summary))
		return []string{}
	}
	for _, httpApi := range res.Items {
		apiId := *httpApi.ApiId
		apiStages, partial := apiWithStage[apiId]
		if partial {
			apiStageMappingV2[apiId] = apiStages
		} else if contains(apiAllStages, apiId) != exclude {
			apiStageMappingV2[apiId] = []string{}
		}
	}
	if !ignoreAccessLogSettings {
		return getLogGroupNamesHttpApisHelper(conn, apiStageMappingV2, exclude, mapDiagnostics)
	}
	return []string{}
}

func getLogGroupNamesRestApisHelper(conn AwsApiGatewayProvider, apiStageMappingRest map[string][]string, exclude bool, ignoreAccessLogSettings bool, mapDiagnostics *MapDiagnostics) []string {
	var logGroupNames []string
	apiGatewayClient := conn.getApiGatewayClient()
	for apiId, apiStages := range apiStageMappingRest {
		res, err := apiGatewayClient.GetStages(context.TODO(), &v1.GetStagesInput{
			RestApiId: &apiId,
		})
		if err != nil {
			summary := fmt.Sprintf("Error while invoking getStages sdk call: %s", err.Error())
			mapDiagnostics.add(errorDiagnostic(summary))
			continue
		}
		for _, stage := range res.Item {
			stageName := *(stage.StageName)
			apiIdWithStageName := strings.Join([]string{apiId, stageName}, "/")
			if len(apiStages) > 0 && contains(apiStages, stageName) == exclude {
				continue
			}
			if settings, ok := stage.MethodSettings["*/*"]; ok {
				if *(settings.LoggingLevel) == "INFO" && settings.DataTraceEnabled {
					logGroupNames = append(logGroupNames, getExecutionLogGroupName(apiId, stageName))
				} else if *(settings.LoggingLevel) == "INFO" {
					mapDiagnostics.addError(FullRequestAndResponseLogNotEnabled.new(), apiIdWithStageName)
				} else if *(settings.LoggingLevel) == "ERROR" {
					mapDiagnostics.addError(ExecutionLogErrorOnly.new(), apiIdWithStageName)
				} else {
					mapDiagnostics.addError(ExecutionLogNotEnabled.new(), apiIdWithStageName)
				}
			}
			if ignoreAccessLogSettings {
				continue
			}
			if stage.AccessLogSettings == nil || stage.AccessLogSettings.DestinationArn == nil {
				mapDiagnostics.addError(AccessLogNotEnabledREST.new(), apiIdWithStageName)
			} else if verifyAccessLogFormat(*(stage.AccessLogSettings.Format), apiIdWithStageName, mapDiagnostics) {
				logGroupNames = append(logGroupNames, getAccessLogGroupNameFromArn(*(stage.AccessLogSettings.DestinationArn)))
			}
		}
	}
	return logGroupNames
}

func getLogGroupNamesHttpApisHelper(conn AwsApiGatewayProvider, apiStageMappingV2 map[string][]string, exclude bool, mapDiagnostics *MapDiagnostics) []string {
	var logGroupNames []string
	apiGatewayV2Client := conn.getApiGatewayV2Client()
	for apiId, apiStages := range apiStageMappingV2 {
		res, err := apiGatewayV2Client.GetStages(context.TODO(), &v2.GetStagesInput{
			ApiId: &apiId,
		})
		if err != nil {
			summary := fmt.Sprintf("Error while invoking getStages sdk call: %s", err.Error())
			mapDiagnostics.add(errorDiagnostic(summary))
			continue
		}
		for _, stage := range res.Items {
			stageName := *(stage.StageName)
			apiIdWithStageName := strings.Join([]string{apiId, stageName}, "/")
			if len(apiStages) > 0 && contains(apiStages, stageName) == exclude {
				continue
			}
			if stage.AccessLogSettings == nil || stage.AccessLogSettings.DestinationArn == nil {
				mapDiagnostics.addError(AccessLogNotEnabledHTTP.new(), apiIdWithStageName)
			} else if verifyAccessLogFormat(*(stage.AccessLogSettings.Format), apiIdWithStageName, mapDiagnostics) {
				logGroupNames = append(logGroupNames, getAccessLogGroupNameFromArn(*(stage.AccessLogSettings.DestinationArn)))
			}
		}
	}
	return logGroupNames
}

func verifyAccessLogFormat(format string, apiIdWithStageName string, mapDiagnostics *MapDiagnostics) bool {
	var parsed map[string]string
	var foundValues []string
	var missingValues []string
	if err := json.Unmarshal([]byte(format), &parsed); err != nil {
		if err = json.Unmarshal([]byte("{"+format+"}"), &parsed); err != nil {
			mapDiagnostics.addError(AccessLogFormatNotJson.new(), apiIdWithStageName)
			return false
		}
	}
	for _, value := range parsed {
		if contains(AccessLogFormatValues, value) {
			foundValues = append(foundValues, value)
		}
	}
	for _, value := range AccessLogFormatValues {
		if !contains(foundValues, value) {
			missingValues = append(missingValues, value)
		}
	}
	if len(missingValues) > 0 {
		mapDiagnostics.addError(AccessLogFormatMissingRequiredValues.new(WithMissingValues(missingValues)), apiIdWithStageName)
		return false
	}
	return true
}
