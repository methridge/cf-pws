package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/hashicorp/vault-plugin-auth-cf/signatures"
	"github.com/hashicorp/vault/api"
)

type weatherCurrent struct {
	Observations []struct {
		StationID         string      `json:"stationID"`
		ObsTimeUtc        time.Time   `json:"obsTimeUtc"`
		ObsTimeLocal      string      `json:"obsTimeLocal"`
		Neighborhood      string      `json:"neighborhood"`
		SoftwareType      string      `json:"softwareType"`
		Country           string      `json:"country"`
		SolarRadiation    float64     `json:"solarRadiation"`
		Lon               float64     `json:"lon"`
		RealtimeFrequency interface{} `json:"realtimeFrequency"`
		Epoch             int         `json:"epoch"`
		Lat               float64     `json:"lat"`
		Uv                float64     `json:"uv"`
		Winddir           int         `json:"winddir"`
		Humidity          int         `json:"humidity"`
		QcStatus          int         `json:"qcStatus"`
		Imperial          struct {
			Temp        int     `json:"temp"`
			HeatIndex   int     `json:"heatIndex"`
			Dewpt       int     `json:"dewpt"`
			WindChill   int     `json:"windChill"`
			WindSpeed   int     `json:"windSpeed"`
			WindGust    int     `json:"windGust"`
			Pressure    float64 `json:"pressure"`
			PrecipRate  float64 `json:"precipRate"`
			PrecipTotal float64 `json:"precipTotal"`
			Elev        int     `json:"elev"`
		} `json:"imperial"`
	} `json:"observations"`
}

var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

func cfAuth(c *api.Client, m map[string]string) (*api.Secret, error) {
	mount, ok := m["mount"]
	if !ok {
		mount = "cf"
	}

	role := m["role"]
	if role == "" {
		return nil, errors.New(`"role" is required`)
	}

	pathToInstanceCert := m["cf_instance_cert"]
	if pathToInstanceCert == "" {
		pathToInstanceCert = os.Getenv("CF_INSTANCE_CERT")
	}
	if pathToInstanceCert == "" {
		return nil, errors.New(`"cf_instance_cert" is required`)
	}

	pathToInstanceKey := m["cf_instance_key"]
	if pathToInstanceKey == "" {
		pathToInstanceKey = os.Getenv("CF_INSTANCE_KEY")
	}
	if pathToInstanceKey == "" {
		return nil, errors.New(`"cf_instance_key" is required`)
	}

	certBytes, err := ioutil.ReadFile(pathToInstanceCert)
	if err != nil {
		return nil, err
	}
	cfInstanceCertContents := string(certBytes)

	signingTime := time.Now().UTC()
	signatureData := &signatures.SignatureData{
		SigningTime:            signingTime,
		Role:                   role,
		CFInstanceCertContents: cfInstanceCertContents,
	}
	signature, err := signatures.Sign(pathToInstanceKey, signatureData)
	if err != nil {
		return nil, err
	}

	loginData := map[string]interface{}{
		"role":             role,
		"cf_instance_cert": cfInstanceCertContents,
		"signing_time":     signingTime.Format(signatures.TimeFormat),
		"signature":        signature,
	}

	path := fmt.Sprintf("auth/%s/login", mount)

	secret, err := c.Logical().Write(path, loginData)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, errors.New("empty response from credential provider")
	}
	return secret, nil
}

func main() {
	vaultAddr := os.Getenv("VAULT_ADDR")
	client, err := api.NewClient(&api.Config{Address: vaultAddr, HttpClient: httpClient})
	if err != nil {
		log.Fatal(err)
	}

	loginData := map[string]string{
		"role": os.Getenv("ROLE"),
	}

	vaultSecret, err := cfAuth(client, loginData)
	if err != nil {
		log.Fatal(err)
	}
	client.SetToken(vaultSecret.Auth.ClientToken)

	data, err := client.Logical().Read("kv/pws")
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		url := fmt.Sprintf("%s?stationId=%s&format=json&units=%s&apiKey=%s",
			data.Data["api"],
			data.Data["sid"],
			data.Data["units"],
			data.Data["key"],
		)

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			fmt.Print(err.Error())
		}
		req.Header.Add("Accept", "application/json")
		req.Header.Add("Content-Type", "application/json")
		resp, err := httpClient.Do(req)
		if err != nil {
			fmt.Print(err.Error())
		}
		defer resp.Body.Close()
		bodyBytes, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			fmt.Print(err.Error())
		}
		var responseObject weatherCurrent
		json.Unmarshal(bodyBytes, &responseObject)
		// fmt.Fprintf(w, "API Response as struct %+v\n", responseObject)
		timeLayout := "15:04:05"
		tz := fmt.Sprintf("%s", data.Data["tz"])
		loc, err := time.LoadLocation(tz)
		localTime := responseObject.Observations[0].ObsTimeUtc.In(loc)
		compassDirs := []string{"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE", "S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW", "N"}
		fmt.Fprintf(w, "Current Conditions for %s at %s are:\n",
			responseObject.Observations[0].StationID,
			localTime.Format(timeLayout),
		)
		fmt.Fprintf(w, "Current:    %d\u00B0F (%d\u00B0C)\n",
			responseObject.Observations[0].Imperial.Temp,
			(((responseObject.Observations[0].Imperial.Temp - 32) * 5) / 9),
		)
		if responseObject.Observations[0].Imperial.Temp > 70 {
			fmt.Fprintf(w, "Feels Like: %d\u00B0F (%d\u00B0C)\n",
				responseObject.Observations[0].Imperial.HeatIndex,
				(((responseObject.Observations[0].Imperial.HeatIndex - 32) * 5) / 9),
			)
		} else {
			fmt.Fprintf(w, "Feels Like: %d\u00B0F (%d\u00B0C)\n",
				responseObject.Observations[0].Imperial.WindChill,
				(((responseObject.Observations[0].Imperial.WindChill - 32) * 5) / 9),
			)
		}
		fmt.Fprintf(w, "Dew Point:  %d\u00B0F (%d\u00B0C)\n",
			responseObject.Observations[0].Imperial.Dewpt,
			(((responseObject.Observations[0].Imperial.Dewpt - 32) * 5) / 9),
		)
		fmt.Fprintf(w, "Humidity:   %d%%\n",
			responseObject.Observations[0].Humidity,
		)
		compassIndex := responseObject.Observations[0].Winddir / 22
		fmt.Fprintf(w, "Wind:       %s(%d\u00B0) @ %d-%d mph\n",
			compassDirs[compassIndex],
			responseObject.Observations[0].Winddir,
			responseObject.Observations[0].Imperial.WindSpeed,
			responseObject.Observations[0].Imperial.WindGust,
		)
	})
	fmt.Println(http.ListenAndServe(":8080", nil))
}
