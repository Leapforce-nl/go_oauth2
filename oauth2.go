package googleoauth2

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	bigquerytools "github.com/Leapforce-nl/go_bigquerytools"
	types "github.com/Leapforce-nl/go_types"
	"github.com/getsentry/sentry-go"
	"google.golang.org/api/iterator"
)

const (
	tableRefreshToken string = "leapforce.oauth2"
)

// OAuth2 stores OAuth2 configuration
//
type OAuth2 struct {
	// config
	apiName         string
	clientID        string
	clientSecret    string
	scope           string
	redirectURL     string
	authURL         string
	tokenURL        string
	tokenHttpMethod string
	Token           *Token
	bigQuery        *bigquerytools.BigQuery
	isLive          bool
}

var tokenMutex sync.Mutex

type Token struct {
	AccessToken  *string `json:"access_token"`
	Scope        *string `json:"scope"`
	TokenType    *string `json:"token_type"`
	ExpiresIn    *int64  `json:"expires_in"`
	RefreshToken *string `json:"refresh_token"`
	Expiry       *time.Time
}

func (t *Token) Print() {
	if t == nil {
		fmt.Println("Token: <nil>")
		return
	}

	if t.AccessToken == nil {
		fmt.Println("AccessToken: <nil>")
	} else {
		fmt.Println("AccessToken: ", *t.AccessToken)
	}

	if t.Scope == nil {
		fmt.Println("Scope: <nil>")
	} else {
		fmt.Println("Scope: ", *t.Scope)
	}

	if t.TokenType == nil {
		fmt.Println("TokenType: <nil>")
	} else {
		fmt.Println("TokenType: ", *t.TokenType)
	}

	if t.ExpiresIn == nil {
		fmt.Println("ExpiresIn: <nil>")
	} else {
		fmt.Println("ExpiresIn: ", *t.ExpiresIn)
	}

	if t.RefreshToken == nil {
		fmt.Println("RefreshToken: <nil>")
	} else {
		fmt.Println("RefreshToken: ", *t.RefreshToken)
	}

	if t.Expiry == nil {
		fmt.Println("Expiry: <nil>")
	} else {
		fmt.Println("Expiry: ", *t.Expiry)
	}
}

type ApiError struct {
	Error       string `json:"error"`
	Description string `json:"error_description,omitempty"`
}

func NewOAuth(apiName string, clientID string, clienSecret string, scope string, redirectURL string, authURL string, tokenURL string, tokenHttpMethod string, bigquery *bigquerytools.BigQuery, isLive bool) *OAuth2 {
	_oAuth2 := new(OAuth2)
	_oAuth2.apiName = apiName
	_oAuth2.clientID = clientID
	_oAuth2.clientSecret = clienSecret
	_oAuth2.scope = scope
	_oAuth2.redirectURL = redirectURL
	_oAuth2.authURL = authURL
	_oAuth2.tokenURL = tokenURL
	_oAuth2.tokenHttpMethod = tokenHttpMethod
	//_oAuth2.Token        *Token
	_oAuth2.bigQuery = bigquery
	_oAuth2.isLive = isLive

	return _oAuth2
}

func (oa *OAuth2) LockToken() {
	tokenMutex.Lock()
}

func (oa *OAuth2) UnlockToken() {
	tokenMutex.Unlock()
}

func (t *Token) Useable() bool {
	if t == nil {
		return false
	}
	if t.AccessToken == nil {
		return false
	}
	if *t.AccessToken == "" {
		if t.RefreshToken == nil {
			return false
		} else if *t.RefreshToken == "" {
			return false
		}
	}
	return true
}

func (t *Token) Refreshable() bool {
	if t == nil {
		return false
	}
	if t.RefreshToken == nil {
		return false
	}
	if *t.RefreshToken == "" {
		return false
	}
	return true
}

func (t *Token) IsExpired() (bool, error) {
	if !t.Useable() {
		return true, &types.ErrorString{"Token is not valid."}
	}
	if t.Expiry.Add(-60 * time.Second).Before(time.Now()) {
		return true, nil
	}
	return false, nil
}

func (oa *OAuth2) GetToken(url string) error {
	guid := types.NewGUID()
	fmt.Println("GetTokenGUID:", guid)
	fmt.Println(url)

	httpClient := http.Client{}
	req, err := http.NewRequest(oa.tokenHttpMethod, url, nil)
	req.Header.Add("Content-Type", "application/json")
	//req.Header.Add("Content-Length", strconv.Itoa(len(data.Encode())))
	if err != nil {
		return err
	}

	// We set this header since we want the response
	// as JSON
	req.Header.Set("accept", "application/json")

	// Send out the HTTP request
	res, err := httpClient.Do(req)
	if err != nil {
		return err
	}

	defer res.Body.Close()

	b, err := ioutil.ReadAll(res.Body)

	if res.StatusCode < 200 || res.StatusCode > 299 {
		fmt.Println("GetTokenGUID:", guid)
		fmt.Println("AccessToken:", oa.Token.AccessToken)
		fmt.Println("RefreshToken:", oa.Token.RefreshToken)
		fmt.Println("ExpiresIn:", oa.Token.ExpiresIn)
		fmt.Println("Expiry:", oa.Token.Expiry)
		fmt.Println("Now:", time.Now())

		eoError := ApiError{}

		err = json.Unmarshal(b, &eoError)
		if err != nil {
			return err
		}

		message := fmt.Sprintln("Error:", res.StatusCode, eoError.Error, ", ", eoError.Description)
		fmt.Println(message)

		if res.StatusCode == 401 {
			if oa.isLive {
				sentry.CaptureMessage(fmt.Sprintf("%s refreshtoken not valid, login needed to retrieve a new one. Error: %s", oa.apiName, message))
			}
			oa.initToken()
		}

		return &types.ErrorString{fmt.Sprintf("Server returned statuscode %v, url: %s", res.StatusCode, req.URL)}
	}

	token := Token{}

	err = json.Unmarshal(b, &token)
	if err != nil {
		log.Println(err)
		return err
	}

	if token.ExpiresIn != nil {
		expiry := time.Now().Add(time.Duration(*token.ExpiresIn) * time.Second)
		token.Expiry = &expiry
	} else {
		token.Expiry = nil
	}

	token.Print()

	oa.Token = &token
	/*
		if oa.Token == nil {
			oa.Token = &Token{}
		}

		oa.Token.Expiry = token.Expiry
		oa.Token.AccessToken = token.AccessToken

		if hasRefreshToken {
			oa.Token.RefreshToken = token.RefreshToken

			err = oa.saveTokenToBigQuery()
			if err != nil {
				return err
			}
		}*/
	err = oa.saveTokenToBigQuery()
	if err != nil {
		return err
	}

	fmt.Println("new AccessToken:")
	fmt.Println(oa.Token.AccessToken)
	fmt.Println("new RefreshToken:")
	fmt.Println(oa.Token.RefreshToken)
	fmt.Println("new ExpiresIn:")
	fmt.Println(oa.Token.ExpiresIn)
	fmt.Println("new Expiry:")
	fmt.Println(oa.Token.Expiry)
	fmt.Println("GetTokenGUID:", guid)

	return nil
}

func (oa *OAuth2) getTokenFromCode(code string) error {
	//fmt.Println("getTokenFromCode")
	url2 := fmt.Sprintf("%s?code=%s&redirect_uri=%s&client_id=%s&client_secret=%s&scope=&grant_type=authorization_code", oa.tokenURL, code, url.PathEscape(oa.redirectURL), oa.clientID, oa.clientSecret)
	//fmt.Println("getTokenFromCode", url)
	return oa.GetToken(url2)
}

func (oa *OAuth2) getTokenFromRefreshToken() error {
	fmt.Println("***getTokenFromRefreshToken***")

	//always get refresh token from BQ prior to using it
	oa.getTokenFromBigQuery()

	if !oa.Token.Refreshable() {
		return oa.initToken()
	}

	url2 := fmt.Sprintf("%s?client_id=%s&client_secret=%s&refresh_token=%s&grant_type=refresh_token&access_type=offline&prompt=consent", oa.tokenURL, oa.clientID, oa.clientSecret, oa.Token.RefreshToken)
	//fmt.Println("getTokenFromRefreshToken", url)
	return oa.GetToken(url2)
}

// ValidateToken validates current token and retrieves a new one if necessary
//
func (oa *OAuth2) ValidateToken() error {
	oa.LockToken()
	defer oa.UnlockToken()

	if !oa.Token.Useable() {

		err := oa.getTokenFromRefreshToken()
		if err != nil {
			return err
		}

		if !oa.Token.Useable() {
			if oa.isLive {
				sentry.CaptureMessage("Refreshtoken not found or empty, login needed to retrieve a new one.")
			}
			err := oa.initToken()
			if err != nil {
				return err
			}
			//return &types.ErrorString{""}
		}
	}

	isExpired, err := oa.Token.IsExpired()
	if err != nil {
		return err
	}
	if isExpired {
		//fmt.Println(time.Now(), "[token expired]")
		err = oa.getTokenFromRefreshToken()
		if err != nil {
			return err
		}
	}

	return nil
}

func (oa *OAuth2) initToken() error {

	if oa == nil {
		return &types.ErrorString{fmt.Sprintf("%s variable not initialized", oa.apiName)}
	}

	url2 := fmt.Sprintf("%s?client_id=%s&response_type=code&redirect_uri=%s&scope=%s&access_type=offline&prompt=consent", oa.authURL, oa.clientID, url.PathEscape(oa.redirectURL), url.PathEscape(oa.scope))

	fmt.Println("Go to this url to get new access token:\n")
	fmt.Println(url2 + "\n")

	// Create a new redirect route
	http.HandleFunc("/oauth/redirect", func(w http.ResponseWriter, r *http.Request) {
		//
		// get authorization code
		//
		err := r.ParseForm()
		if err != nil {
			fmt.Fprintf(os.Stdout, "could not parse query: %v", err)
			w.WriteHeader(http.StatusBadRequest)
		}
		code := r.FormValue("code")

		fmt.Println(code)

		err = oa.getTokenFromCode(code)
		if err != nil {
			fmt.Println(err)
		}

		w.WriteHeader(http.StatusFound)

		return
	})

	http.ListenAndServe(":8080", nil)

	return nil
}

func (oa *OAuth2) getTokenFromBigQuery() error {
	fmt.Println("***getTokenFromBigQuery***")
	// create client
	bqClient, err := oa.bigQuery.CreateClient()
	if err != nil {
		fmt.Println("\nerror in BigQueryCreateClient")
		return err
	}

	ctx := context.Background()

	sql := fmt.Sprintf("SELECT TokenType, AccessToken, RefreshToken, Expiry, Scope FROM `%s` WHERE Api = '%s' AND ClientID = '%s'", tableRefreshToken, oa.apiName, oa.clientID)
	//fmt.Println(sql)

	q := bqClient.Query(sql)
	it, err := q.Read(ctx)
	if err != nil {
		return err
	}

	token := new(Token)

	for {
		err := it.Next(token)
		if err == iterator.Done {
			break
		}
		if err != nil {
			return err
		}

		break
	}

	oa.Token = token

	oa.Token.Print()
	/*
		if oa.Token == nil {
			oa.Token = new(Token)
		}

		//tokenType := "bearer"
		//oa.Token.TokenType = &tokenType
		//expiry := time.Now().Add(-10 * time.Second)
		//oa.Token.Expiry = &expiry
		oa.Token.RefreshToken = token.RefreshToken
		oa.Token.AccessToken = nil*/

	return nil
}

func (oa *OAuth2) saveTokenToBigQuery() error {
	// create client
	bqClient, err := oa.bigQuery.CreateClient()
	if err != nil {
		fmt.Println("\nerror in BigQueryCreateClient")
		return err
	}

	ctx := context.Background()

	tokenType := "NULLIF('','')"
	if oa.Token.TokenType != nil {
		if *oa.Token.TokenType != "" {
			tokenType = fmt.Sprintf("'%s'", *oa.Token.TokenType)
		}
	}

	accessToken := "NULLIF('','')"
	if oa.Token.AccessToken != nil {
		if *oa.Token.AccessToken != "" {
			accessToken = fmt.Sprintf("'%s'", *oa.Token.AccessToken)
		}
	}

	refreshToken := "NULLIF('','')"
	if oa.Token.RefreshToken != nil {
		if *oa.Token.RefreshToken != "" {
			refreshToken = fmt.Sprintf("'%s'", *oa.Token.RefreshToken)
		}
	}

	expiry := "TIMESTAMP(NULL)"
	if oa.Token.Expiry != nil {
		expiry = fmt.Sprintf("TIMESTAMP('%s')", (*oa.Token.Expiry).Format("2006-01-02T15:04:05"))
	}

	scope := "NULLIF('','')"
	if oa.Token.Scope != nil {
		if *oa.Token.Scope != "" {
			scope = fmt.Sprintf("'%s'", *oa.Token.Scope)
		}
	}

	sql := "MERGE `" + tableRefreshToken + "` AS TARGET " +
		"USING  (SELECT '" +
		oa.apiName + "' AS Api,'" +
		oa.clientID + "' AS ClientID," +
		tokenType + " AS TokenType," +
		accessToken + " AS AccessToken," +
		refreshToken + " AS RefreshToken," +
		expiry + " AS Expiry," +
		scope + " AS Scope) AS SOURCE " +
		" ON TARGET.Api = SOURCE.Api " +
		" AND TARGET.ClientID = SOURCE.ClientID " +
		"WHEN MATCHED THEN " +
		"	UPDATE " +
		"	SET TokenType = SOURCE.TokenType " +
		"	, AccessToken = SOURCE.AccessToken " +
		"	, RefreshToken = SOURCE.RefreshToken " +
		"	, Expiry = SOURCE.Expiry " +
		"	, Scope = SOURCE.Scope	 " +
		"WHEN NOT MATCHED BY TARGET THEN " +
		"	INSERT (Api, ClientID, TokenType, AccessToken, RefreshToken, Expiry, Scope) " +
		"	VALUES (SOURCE.Api, SOURCE.ClientID, SOURCE.TokenType, SOURCE.AccessToken, SOURCE.RefreshToken, SOURCE.Expiry, SOURCE.Scope)"

	q := bqClient.Query(sql)
	//fmt.Println(sql)

	job, err := q.Run(ctx)
	if err != nil {
		return err
	}

	for {
		status, err := job.Status(ctx)
		if err != nil {
			return err
		}
		if status.Done() {
			if status.Err() != nil {
				return status.Err()
			}
			break
		}
		time.Sleep(1 * time.Second)
	}

	return nil
}
