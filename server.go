package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var ownedGamePlaytimeGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Namespace: "steam",
	Subsystem: "owned_games",
	Name:      "playtime_seconds",
	Help:      "Amount of time an owned games is played forever",
}, []string{"app_id", "name", "steam_id"})

var achievementGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Namespace: "steam",
	Subsystem: "achievements",
	Name:      "achieved",
	Help:      "Whether an achievement has been achieved (1) or not (0)",
}, []string{"app_id", "game_name", "achievement_name", "steam_id"})

const API_ORIGIN = "https://api.steampowered.com"
const OWNED_GAMES_ENDPOINT = "/IPlayerService/GetOwnedGames/v0001/"
const ACHIEVEMENTS_ENDPOINT = "/ISteamUserStats/GetUserStatsForGame/v0002/"
const GLOBAL_ACHIEVEMENTS_ENDPOINT = "/ISteamUserStats/GetGlobalAchievementPercentagesForApp/v0002/"

var achievementCache = NewAchievementCache()

func initMetrics() {
	prometheus.MustRegister(ownedGamePlaytimeGauge)
	prometheus.MustRegister(achievementGauge)
}

func printUsageExit() {
	flag.Usage()
	os.Exit(1)
}

func main() {

	var portPtr = flag.Int("port", 8000, "A port number")
	var sleepPtr = flag.Int("sleep", 300, "How long to sleep in seconds")

	flag.Usage = func() {
		fmt.Printf("%s userid\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Printf("\nENVIRONMENT\n")
		fmt.Printf("\tSTEAM_KEY\tAn API Key to make Steam requests. Mandatory.\n")
	}
	flag.Parse()

	var key = os.Getenv("STEAM_KEY")
	if len(key) == 0 {
		printUsageExit()
	}

	if flag.NArg() < 1 {
		printUsageExit()
	}

	var user = flag.Arg(0)

	initMetrics()

	sleepDuration := time.Duration(int64(*sleepPtr)) * time.Second
	pollApiForMetrics(sleepDuration, key, user)

	addr := fmt.Sprintf(":%d", *portPtr)
	fmt.Printf("Running on http://localhost%s\n", addr)

	http.Handle("/", FrontPageHandler{})
	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(addr, nil)
}

type FrontPageHandler struct{}

func (h FrontPageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(`<body>
        <h1>Steam Exporter</h1>
        <p><a href="/metrics">See Metrics</a></p>
        </body>`))
}

func getJson(url string, key string, user string, target interface{}) error {
	println("Fetching JSON")
	var myClient = &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	q := req.URL.Query()
	q.Add("key", key)
	q.Add("steamid", user)
	q.Add("format", "json")
	q.Add("include_appinfo", "true")
	q.Add("include_played_free_games", "true")
	req.URL.RawQuery = q.Encode()

	fmt.Printf("Request URL: %s\n", req.URL.String())

	r, err := myClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer r.Body.Close()

	// Read the body first so we can include it in error messages
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	// Handle specific status codes
	switch r.StatusCode {
	case http.StatusOK:
		// Continue with JSON parsing
	case http.StatusTooManyRequests:

		return fmt.Errorf("rate limited by Steam API (429)")
	case http.StatusUnauthorized:
		return fmt.Errorf("unauthorized (401) - check your Steam API key")
	case http.StatusForbidden:
		return fmt.Errorf("forbidden (403) - check your Steam API key and permissions")
	default:
		return fmt.Errorf("unexpected status code %d: %s", r.StatusCode, string(body))
	}

	// Check if the response starts with HTML (common error case)
	if len(body) > 0 && body[0] == '<' {
		return fmt.Errorf("received HTML instead of JSON. Response: %s", string(body))
	}

	// Try to decode the JSON
	err = json.NewDecoder(bytes.NewReader(body)).Decode(target)
	if err != nil {
		return fmt.Errorf("failed to decode JSON: %w, body: %s", err, string(body))
	}

	return nil
}

func reportOwnedGame(game OwnedGame, userId string) {
	// Prometheus prefers seconds rather than minutes
	var playtimeSeconds = float64(60 * game.PlaytimeForever)
	ownedGamePlaytimeGauge.With(prometheus.Labels{
		"name":     game.Name,
		"app_id":   strconv.FormatUint(game.AppId, 10),
		"steam_id": userId,
	}).Set(playtimeSeconds)
}

func callSteamGetOwnedGames(key string, user string) (OwnedGamedResponse, error) {
	ownedGamesHttpResponse := OwnedGamesHttpResponse{}
	url := fmt.Sprintf("%s%s", API_ORIGIN, OWNED_GAMES_ENDPOINT)
	if err := getJson(url, key, user, &ownedGamesHttpResponse); err != nil {
		resp := OwnedGamedResponse{}
		return resp, err
	}
	return ownedGamesHttpResponse.Response, nil
}

func callSteamGetAchievements(key string, user string, appId uint64) (AchievementResponse, error) {
	achievementResponse := AchievementResponse{}
	url := fmt.Sprintf("%s%s", API_ORIGIN, ACHIEVEMENTS_ENDPOINT)

	// Create a new client with timeout
	myClient := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return achievementResponse, err
	}

	q := req.URL.Query()
	q.Add("key", key)
	q.Add("steamid", user)
	q.Add("appid", strconv.FormatUint(appId, 10))
	req.URL.RawQuery = q.Encode()

	// Debug: Print the full request URL
	fmt.Printf("Achievement Request URL: %s\n", req.URL.String())

	r, err := myClient.Do(req)
	if err != nil {
		fmt.Printf("Achievement Request Error: %v\n", err)
		return achievementResponse, err
	}
	defer r.Body.Close()

	// Debug: Print response status
	fmt.Printf("Achievement Response Status: %s\n", r.Status)

	// Debug: Read and print response body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		fmt.Printf("Error reading response body: %v\n", err)
		return achievementResponse, err
	}
	fmt.Printf("Achievement Response Body: %s\n", string(bodyBytes))

	// Create a new reader from the body bytes since we've already read it
	err = json.NewDecoder(bytes.NewReader(bodyBytes)).Decode(&achievementResponse)
	if err != nil {
		fmt.Printf("Error decoding response: %v\n", err)
		return achievementResponse, err
	}
	fmt.Printf("Achievements Decoded: %+v\n", achievementResponse)

	return achievementResponse, err
}

func callSteamGetGlobalAchievements(appId uint64) (GlobalAchievementResponse, error) {
	globalAchievementResponse := GlobalAchievementResponse{}
	url := fmt.Sprintf("%s%s", API_ORIGIN, GLOBAL_ACHIEVEMENTS_ENDPOINT)

	// Create a new client with timeout
	myClient := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return globalAchievementResponse, err
	}

	q := req.URL.Query()
	q.Add("gameid", strconv.FormatUint(appId, 10))
	req.URL.RawQuery = q.Encode()

	// Debug: Print the full request URL
	fmt.Printf("Global Achievement Request URL: %s\n", req.URL.String())

	r, err := myClient.Do(req)
	if err != nil {
		fmt.Printf("Global Achievement Request Error: %v\n", err)
		return globalAchievementResponse, err
	}
	defer r.Body.Close()

	// Debug: Print response status
	fmt.Printf("Global Achievement Response Status: %s\n", r.Status)

	// Debug: Read and print response body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		fmt.Printf("Error reading response body: %v\n", err)
		return globalAchievementResponse, err
	}
	fmt.Printf("Global Achievement Response Body: %s\n", string(bodyBytes))

	// Create a new reader from the body bytes since we've already read it
	err = json.NewDecoder(bytes.NewReader(bodyBytes)).Decode(&globalAchievementResponse)
	if err != nil {
		fmt.Printf("Error decoding response: %v\n", err)
		return globalAchievementResponse, err
	}

	return globalAchievementResponse, nil
}

func reportAchievements(userAchievements []Achievement, globalAchievements []GlobalAchievement, gameName string, appId uint64, userId string) {
	// Create a map of user achievements for quick lookup
	userAchievementMap := make(map[string]int)
	for _, achievement := range userAchievements {
		userAchievementMap[achievement.Name] = achievement.Achieved
	}

	// Report all achievements, using 0 for unearned ones
	for _, globalAchievement := range globalAchievements {
		achieved := 0
		if earned, exists := userAchievementMap[globalAchievement.Name]; exists {
			achieved = earned
		}
		achievementGauge.With(prometheus.Labels{
			"game_name":        gameName,
			"app_id":           strconv.FormatUint(appId, 10),
			"achievement_name": globalAchievement.Name,
			"steam_id":         userId,
		}).Set(float64(achieved))
	}
}

func pollApiForMetrics(sleep time.Duration, key string, user string) {
	go func() {
		for {
			resp, err := callSteamGetOwnedGames(key, user)
			if err != nil {
				fmt.Println("Retrieval Error: ", err)
				time.Sleep(sleep)
				continue
			}

			fmt.Printf("Found Games: %+v\n", resp.GameCount)

			for _, game := range resp.Games {
				reportOwnedGame(game, user)

				// Skip achievement fetching for games with zero playtime
				if game.PlaytimeForever == 0 {
					fmt.Printf("Skipping achievements for %s (0 playtime)\n", game.Name)
					continue
				}

				// Get global achievements from cache or fetch them
				var globalAchievements []GlobalAchievement
				if globalEntry, exists := achievementCache.GetGlobalAchievements(game.AppId); exists {
					fmt.Printf("Using cached global achievements for %s\n", game.Name)
					globalAchievements = globalEntry.GlobalAchievements
				} else {
					// Fetch global achievements
					globalAchievementResp, err := callSteamGetGlobalAchievements(game.AppId)
					if err != nil {
						fmt.Printf("Error fetching global achievements for game %s: %v\n", game.Name, err)
						continue
					}
					fmt.Printf("RAW Global Achievements: %+v\n", globalAchievementResp)
					globalAchievements = globalAchievementResp.AchievementPercentages.Achievements
					achievementCache.SetGlobalAchievements(game.AppId, globalAchievements)
				}

				// Check if we need to invalidate the user achievements cache
				var userAchievements []Achievement
				if !achievementCache.ShouldInvalidateUserCache(game.AppId, game.PlaytimeForever) {
					// Use cached user achievements
					if userEntry, exists := achievementCache.GetUserAchievements(game.AppId); exists {
						fmt.Printf("Using cached user achievements for %s\n", game.Name)
						userAchievements = userEntry.UserAchievements
					}
				}

				// If we don't have cached user achievements, fetch them
				if userAchievements == nil {
					// Add a small delay between achievement requests to avoid rate limiting
					time.Sleep(1 * time.Second)

					// Fetch user achievements
					achievementResp, err := callSteamGetAchievements(key, user, game.AppId)
					if err != nil {
						fmt.Printf("Error fetching achievements for game %s: %v\n", game.Name, err)
						continue
					}
					fmt.Printf("RAW Achievements: %+v\n", achievementResp)
					userAchievements = achievementResp.PlayerStats.Achievements
					achievementCache.SetUserAchievements(game.AppId, userAchievements, game.PlaytimeForever)
				}

				reportAchievements(
					userAchievements,
					globalAchievements,
					game.Name,
					game.AppId,
					user,
				)
			}
			time.Sleep(sleep)
		}
	}()
}
