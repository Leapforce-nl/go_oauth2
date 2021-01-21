package oauth2

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"reflect"
	"strings"

	errortools "github.com/leapforce-libraries/go_errortools"
	utilities "github.com/leapforce-libraries/go_utilities"
)

type RequestConfig struct {
	URL                string
	BodyModel          interface{}
	ResponseModel      interface{}
	ErrorModel         interface{}
	NonDefaultHeaders  *http.Header
	SkipAccessToken    *bool
	XWWWFormURLEncoded *bool
}

// Get returns http.Response for generic oAuth2 Get http call
//
func (oa *OAuth2) Get(config *RequestConfig) (*http.Request, *http.Response, *errortools.Error) {
	return oa.httpRequest(http.MethodGet, config)
}

// Post returns http.Response for generic oAuth2 Post http call
//
func (oa *OAuth2) Post(config *RequestConfig) (*http.Request, *http.Response, *errortools.Error) {
	return oa.httpRequest(http.MethodPost, config)
}

// Put returns http.Response for generic oAuth2 Put http call
//
func (oa *OAuth2) Put(config *RequestConfig) (*http.Request, *http.Response, *errortools.Error) {
	return oa.httpRequest(http.MethodPut, config)
}

// Patch returns http.Response for generic oAuth2 Patch http call
//
func (oa *OAuth2) Patch(config *RequestConfig) (*http.Request, *http.Response, *errortools.Error) {
	return oa.httpRequest(http.MethodPatch, config)
}

// Delete returns http.Response for generic oAuth2 Delete http call
//
func (oa *OAuth2) Delete(config *RequestConfig) (*http.Request, *http.Response, *errortools.Error) {
	return oa.httpRequest(http.MethodDelete, config)
}

// HTTP returns http.Response for generic oAuth2 http call
//
func (oa *OAuth2) HTTP(httpMethod string, config *RequestConfig) (*http.Request, *http.Response, *errortools.Error) {
	return oa.httpRequest(httpMethod, config)
}

func (oa *OAuth2) httpRequest(httpMethod string, config *RequestConfig) (*http.Request, *http.Response, *errortools.Error) {
	if config == nil {
		return nil, nil, errortools.ErrorMessage("Request config may not be a nil pointer.")
	}

	if utilities.IsNil(config.BodyModel) {
		return oa.httpRequestFromReader(httpMethod, config, nil)
	}

	if config.XWWWFormURLEncoded != nil {
		if *config.XWWWFormURLEncoded {
			tag := "json"
			url, e := utilities.StructToURL(&config.BodyModel, &tag)
			if e != nil {
				return nil, nil, e
			}

			return oa.httpRequestFromReader(httpMethod, config, strings.NewReader(*url))
		}
	}

	b, err := json.Marshal(config.BodyModel)
	if err != nil {
		return nil, nil, errortools.ErrorMessage(err)
	}

	return oa.httpRequestFromReader(httpMethod, config, bytes.NewBuffer(b))
}

func (oa *OAuth2) httpRequestFromReader(httpMethod string, config *RequestConfig, reader io.Reader) (*http.Request, *http.Response, *errortools.Error) {
	var err error
	var e *errortools.Error
	var request *http.Request
	var response *http.Response
	var accessToken string

	request, err = http.NewRequest(httpMethod, config.URL, reader)
	if err != nil {
		e.SetMessage(err)
		goto exit
	}

	// default headers
	request.Header.Set("Accept", "application/json")
	if reader != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	// overrule with input headers
	if config.NonDefaultHeaders != nil {
		for key, values := range *config.NonDefaultHeaders {
			request.Header.Del(key) //delete old header
			for _, value := range values {
				request.Header.Add(key, value) //add new header(s)
			}
		}
	}

	// Authorization header
	if config.SkipAccessToken != nil {
		if *config.SkipAccessToken {
			goto tokenSkipped
		}
	}

	_, e = oa.ValidateToken()
	if e != nil {
		goto exit
	}

	if oa.token == nil {
		e.SetMessage("No Token.")
		goto exit
	}

	if (*oa.token).AccessToken == nil {
		e.SetMessage("No AccessToken.")
		goto exit
	}

	accessToken = *((*oa.token).AccessToken)
	request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))

tokenSkipped:

	// Send out the HTTP request
	response, e = utilities.DoWithRetry(new(http.Client), request, oa.maxRetries, oa.secondsBetweenRetries)
	if response != nil {
		// Check HTTP StatusCode
		if response.StatusCode < 200 || response.StatusCode > 299 {
			fmt.Println(fmt.Sprintf("ERROR in %s", httpMethod))
			fmt.Println("url", config.URL)
			fmt.Println("StatusCode", response.StatusCode)
			fmt.Println(accessToken)

			if e == nil {
				e = new(errortools.Error)
			}

			e.SetMessage(fmt.Sprintf("Server returned statuscode %v", response.StatusCode))
		}
	}

	if response.Body == nil {
		goto exit
	}

	if e != nil {
		if !utilities.IsNil(config.ErrorModel) {
			err := oa.unmarshalError(response, config.ErrorModel)
			errortools.CaptureInfo(err)
		}
		goto exit
	}

	if !utilities.IsNil(config.ResponseModel) {
		defer response.Body.Close()

		b, err := ioutil.ReadAll(response.Body)
		if err != nil {
			e.SetMessage(err)
			goto exit
		}

		err = json.Unmarshal(b, &config.ResponseModel)
		if err != nil {
			e.SetMessage(err)
			goto exit
		}
	}

exit:
	if e != nil {
		e.SetRequest(request)
		e.SetResponse(response)
	}
	return request, response, e
}

func (oa *OAuth2) unmarshalError(response *http.Response, errorModel interface{}) *errortools.Error {
	if response == nil {
		return nil
	}
	if reflect.TypeOf(errorModel).Kind() != reflect.Ptr {
		return errortools.ErrorMessage("Type of errorModel must be a pointer.")
	}
	if reflect.ValueOf(errorModel).IsNil() {
		return nil
	}

	defer response.Body.Close()

	b, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return errortools.ErrorMessage(err)
	}

	err = json.Unmarshal(b, &errorModel)
	if err != nil {
		return errortools.ErrorMessage(err)
	}

	return nil
}
