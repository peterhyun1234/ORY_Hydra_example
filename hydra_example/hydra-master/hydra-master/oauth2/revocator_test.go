/*
 * Copyright © 2015-2018 Aeneas Rekkas <aeneas+oss@aeneas.io>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * @author		Aeneas Rekkas <aeneas+oss@aeneas.io>
 * @copyright 	2015-2018 Aeneas Rekkas <aeneas+oss@aeneas.io>
 * @license 	Apache-2.0
 */

package oauth2_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ory/hydra/internal/httpclient/client/public"
	"github.com/ory/x/urlx"

	"github.com/ory/hydra/internal"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	httptransport "github.com/go-openapi/runtime/client"

	"github.com/ory/fosite"
	hydra "github.com/ory/hydra/internal/httpclient/client"
	"github.com/ory/hydra/oauth2"
	"github.com/ory/hydra/x"
)

func createAccessTokenSession(subject, client string, token string, expiresAt time.Time, fs x.FositeStorer, scopes fosite.Arguments) {
	createAccessTokenSessionPairwise(subject, client, token, expiresAt, fs, scopes, "")
}

func createAccessTokenSessionPairwise(subject, client string, token string, expiresAt time.Time, fs x.FositeStorer, scopes fosite.Arguments, obfuscated string) {
	ar := fosite.NewAccessRequest(oauth2.NewSession(subject))
	ar.GrantedScope = fosite.Arguments{"core"}
	if scopes != nil {
		ar.GrantedScope = scopes
	}
	ar.RequestedAt = time.Now().UTC().Round(time.Minute)
	ar.Client = &fosite.DefaultClient{ID: client}
	ar.Session.SetExpiresAt(fosite.AccessToken, expiresAt)
	ar.Session.(*oauth2.Session).Extra = map[string]interface{}{"foo": "bar"}
	if obfuscated != "" {
		ar.Session.(*oauth2.Session).Claims.Subject = obfuscated
	}

	if err := fs.CreateAccessTokenSession(nil, token, ar); err != nil {
		panic(err)
	}
}

func TestRevoke(t *testing.T) {
	conf := internal.NewConfigurationWithDefaults()
	reg := internal.NewRegistry(conf)

	internal.MustEnsureRegistryKeys(reg, x.OpenIDConnectKeyName)
	internal.AddFositeExamples(reg)

	tokens := Tokens(conf, 4)
	now := time.Now().UTC().Round(time.Second)

	handler := reg.OAuth2Handler()
	router := x.NewRouterAdmin()
	handler.SetRoutes(router, router.RouterPublic(), func(h http.Handler) http.Handler {
		return h
	})
	server := httptest.NewServer(router)
	defer server.Close()

	createAccessTokenSession("alice", "my-client", tokens[0][0], now.Add(time.Hour), reg.OAuth2Storage(), nil)
	createAccessTokenSession("siri", "my-client", tokens[1][0], now.Add(time.Hour), reg.OAuth2Storage(), nil)
	createAccessTokenSession("siri", "my-client", tokens[2][0], now.Add(-time.Hour), reg.OAuth2Storage(), nil)
	createAccessTokenSession("siri", "encoded:client", tokens[3][0], now.Add(-time.Hour), reg.OAuth2Storage(), nil)

	require.Len(t, reg.OAuth2Storage().(*oauth2.FositeMemoryStore).AccessTokens, 4)

	client := hydra.NewHTTPClientWithConfig(nil, &hydra.TransportConfig{Schemes: []string{"http"}, Host: urlx.ParseOrPanic(server.URL).Host})

	for k, c := range []struct {
		token  string
		assert func(*testing.T)
	}{
		{
			token: "invalid",
			assert: func(t *testing.T) {
				assert.Len(t, reg.OAuth2Storage().(*oauth2.FositeMemoryStore).AccessTokens, 4)
			},
		},
		{
			token: tokens[3][1],
			assert: func(t *testing.T) {
				assert.Len(t, reg.OAuth2Storage().(*oauth2.FositeMemoryStore).AccessTokens, 4)
			},
		},
		{
			token: tokens[0][1],
			assert: func(t *testing.T) {
				t.Logf("Tried to delete: %s %s", tokens[0][0], tokens[0][1])
				assert.Len(t, reg.OAuth2Storage().(*oauth2.FositeMemoryStore).AccessTokens, 3)
			},
		},
		{
			token: tokens[0][1],
		},
		{
			token: tokens[2][1],
			assert: func(t *testing.T) {
				assert.Len(t, reg.OAuth2Storage().(*oauth2.FositeMemoryStore).AccessTokens, 2)
			},
		},
		{
			token: tokens[1][1],
			assert: func(t *testing.T) {
				assert.Len(t, reg.OAuth2Storage().(*oauth2.FositeMemoryStore).AccessTokens, 1)
			},
		},
	} {
		t.Run(fmt.Sprintf("case=%d", k), func(t *testing.T) {
			//
			//client.Configuration.Username = "my-client"
			//client.Configuration.Password = "foobar"
			_, err := client.Public.RevokeOAuth2Token(
				public.NewRevokeOAuth2TokenParams().WithToken(c.token),
				httptransport.BasicAuth("my-client", "foobar"),
			)
			require.NoError(t, err)

			if c.assert != nil {
				c.assert(t)
			}
		})
	}
}
