package main

type Achievement struct {
	Name     string `json:"name"`
	Achieved int    `json:"achieved"`
}

type PlayerStats struct {
	SteamID      string        `json:"steamID"`
	GameName     string        `json:"gameName"`
	Achievements []Achievement `json:"achievements"`
}

type AchievementResponse struct {
	PlayerStats PlayerStats `json:"playerstats"`
}
