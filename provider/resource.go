package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/Traceableai/terraform-provider-awsapigateway/provider/keys"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	v1 "github.com/aws/aws-sdk-go-v2/service/apigateway"
	v2 "github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func AwsApiGatewayResource() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceCreateUpdate,
		ReadContext:   resourceRead,
		UpdateContext: resourceCreateUpdate,
		DeleteContext: resourceDelete,

		Schema: map[string]*schema.Schema{
			keys.Identifier: {
				Type:     schema.TypeString,
				Optional: true,
				Default:  "",
			},
			keys.IgnoreAccessLogSettings: {
				Type:     schema.TypeBool,
				Optional: true,
				Default:  false,
			},
			keys.LogGroupNames: {
				Type:     schema.TypeList,
				Computed: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
			},
			keys.Accounts: {
				Type:     schema.TypeList,
				Required: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						keys.Region: {
							Type:     schema.TypeString,
							Required: true,
						},
						keys.ApiList: {
							Type:     schema.TypeList,
							Required: true,
							Elem:     &schema.Schema{Type: schema.TypeString},
						},
						keys.CrossAccountRoleArn: {
							Type:     schema.TypeString,
							Required: true,
						},
						keys.Exclude: {
							Type:     schema.TypeBool,
							Required: true,
						},
					},
				},
			},
		},
	}
}

func resourceCreateUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	logGroupNames := make([]string, 0)
	accounts := d.Get(keys.Accounts).([]interface{})

	mapDiagnostics := &MapDiagnostics{
		diagnostics:      diag.Diagnostics{},
		warnDiagnostics:  make(map[string][]string),
		errorDiagnostics: make(map[string][]string),
	}

	conn := meta.(AwsApiGatewayProvider)

	for _, account := range accounts {
		acc := account.(map[string]interface{})
		region := acc[keys.Region].(string)
		apiList := acc[keys.ApiList].([]interface{})
		crossAccRoleArn := acc[keys.CrossAccountRoleArn].(string)
		exclude := acc[keys.Exclude].(bool)

		ignoreAccessLogSettings := d.Get(keys.IgnoreAccessLogSettings).(bool)
		// if cross account role arn is provided, then reinitialise client with an assumed role
		if len(crossAccRoleArn) > 0 {
			cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(region))
			if err != nil {
				mapDiagnostics.add(errorDiagnostic(err.Error()))
				continue
			}

			stsSvc := sts.NewFromConfig(cfg)
			creds := stscreds.NewAssumeRoleProvider(stsSvc, crossAccRoleArn)
			cfg.Credentials = aws.NewCredentialsCache(creds)

			conn = newFromConfig(cfg)
		}

		logGroupNames = append(logGroupNames, getLogGroupNames(apiList, exclude, ignoreAccessLogSettings, conn, mapDiagnostics)...)
	}

	if d.Id() == "" {
		d.SetId(uuid.New().String())
	}
	if err := d.Set(keys.LogGroupNames, logGroupNames); err != nil {
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

func getLogGroupNames(apiGateways []interface{}, exclude bool, ignoreAccessLogSettings bool, conn AwsApiGatewayProvider, mapDiagnostics *MapDiagnostics) []string {
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

	accessLogFormatKeysMap := make(map[string]AccessLogFormatMap)
	logGroupNames := getLogGroupNamesRestApis(conn, apiAllStages, apiWithStage, exclude, ignoreAccessLogSettings, accessLogFormatKeysMap, mapDiagnostics)
	apiGatewayV2LogGroupNames := getLogGroupNamesHttpApis(conn, apiAllStages, apiWithStage, exclude, ignoreAccessLogSettings, accessLogFormatKeysMap, mapDiagnostics)
	return removeDuplicates(append(logGroupNames, apiGatewayV2LogGroupNames...))
}

func getLogGroupNamesRestApis(conn AwsApiGatewayProvider, apiAllStages []string, apiWithStage map[string][]string,
	exclude bool, ignoreAccessLogSettings bool, accessLogFormatKeysMap map[string]AccessLogFormatMap, mapDiagnostics *MapDiagnostics) []string {
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
	logGroupNames := getLogGroupNamesRestApisHelper(conn, apiStageMappingRest, exclude, ignoreAccessLogSettings, accessLogFormatKeysMap, mapDiagnostics)
	return logGroupNames
}

func getLogGroupNamesHttpApis(conn AwsApiGatewayProvider, apiAllStages []string, apiWithStage map[string][]string, exclude bool,
	ignoreAccessLogSettings bool, accessLogFormatKeysMap map[string]AccessLogFormatMap, mapDiagnostics *MapDiagnostics) []string {
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
		return getLogGroupNamesHttpApisHelper(conn, apiStageMappingV2, exclude, accessLogFormatKeysMap, mapDiagnostics)
	}
	return []string{}
}

func getLogGroupNamesRestApisHelper(conn AwsApiGatewayProvider, apiStageMappingRest map[string][]string, exclude bool,
	ignoreAccessLogSettings bool, accessLogFormatKeysMap map[string]AccessLogFormatMap, mapDiagnostics *MapDiagnostics) []string {
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
			} else {
				logGroupName := getAccessLogGroupNameFromArn(*(stage.AccessLogSettings.DestinationArn))
				if verifyAccessLogFormat(*(stage.AccessLogSettings.Format), apiIdWithStageName, logGroupName, accessLogFormatKeysMap, mapDiagnostics) {
					logGroupNames = append(logGroupNames, logGroupName)
				}
			}
		}
	}
	return logGroupNames
}

func getLogGroupNamesHttpApisHelper(conn AwsApiGatewayProvider, apiStageMappingV2 map[string][]string, exclude bool,
	accessLogFormatKeysMap map[string]AccessLogFormatMap, mapDiagnostics *MapDiagnostics) []string {
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
			} else {
				logGroupName := getAccessLogGroupNameFromArn(*(stage.AccessLogSettings.DestinationArn))
				if verifyAccessLogFormat(*(stage.AccessLogSettings.Format), apiIdWithStageName, logGroupName, accessLogFormatKeysMap, mapDiagnostics) {
					logGroupNames = append(logGroupNames, logGroupName)
				}
			}
		}
	}
	return logGroupNames
}

func fixAccessLogFormatMissingQuotes(format string) string {
	re := regexp.MustCompile(`:\s*(\$context[.\w]*)`)
	return string(re.ReplaceAll([]byte(format), []byte(":\"$1\"")))
}

func verifyAccessLogFormat(format string, apiIdWithStageName string, logGroupName string,
	accessLogFormatKeysMap map[string]AccessLogFormatMap, mapDiagnostics *MapDiagnostics) bool {

	var parsed map[string]interface{}
	fixedFormat := fixAccessLogFormatMissingQuotes(format)
	if err := json.Unmarshal([]byte(fixedFormat), &parsed); err != nil {
		if err = json.Unmarshal([]byte("{"+fixedFormat+"}"), &parsed); err != nil {
			mapDiagnostics.addError(AccessLogFormatNotJson.new(), apiIdWithStageName)
			return false
		}
	}

	var foundValues []string
	var missingValues []string
	accessLogKeys := make(map[string]string)
	for key, value := range Flatten(parsed) {
		valueStr := value.(string)
		accessLogKeys[valueStr] = key
		if contains(AccessLogFormatMandatoryValues, valueStr) {
			foundValues = append(foundValues, valueStr)
		}
	}

	for _, value := range AccessLogFormatMandatoryValues {
		if !contains(foundValues, value) {
			missingValues = append(missingValues, value)
		}
	}
	if len(missingValues) > 0 {
		mapDiagnostics.addError(AccessLogFormatMissingRequiredValues.new(WithMissingValues(missingValues)), apiIdWithStageName)
		return false
	}
	if storedMap, found := accessLogFormatKeysMap[logGroupName]; found {
		for value, key := range accessLogKeys {
			if storedKey, valueFound := storedMap.valueToKey[value]; valueFound {
				if key != storedKey {
					mapDiagnostics.addWarn(AccessLogFormatKeyMismatch.new(WithLogGroupName(logGroupName)), value)
				}
			} else {
				storedMap.valueToKey[value] = key
			}
		}
	} else {
		accessLogFormatKeysMap[logGroupName] = AccessLogFormatMap{
			valueToKey: accessLogKeys,
		}
	}
	return true
}
