package main

import (
	"log"
	"strings"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Planning Center service types.
type ServiceTypes struct {
	ID         uint64    `gorm:"primary_key" json:"id"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	ArchivedAt time.Time `json:"archived_at"`
	DeletedAt  time.Time `json:"deleted_at"`
	Name       string    `json:"name"`
}

// Planning Center plans.
type Plans struct {
	ID          uint64    `gorm:"primary_key" json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	SeriesTitle string    `json:"series_title"`
	Title       string    `json:"title"`
	FirstTimeAt time.Time `json:"first_time_at"`
	LastTimeAt  time.Time `json:"last_time_at"`
	MultiDay    bool      `json:"multi_day"`
	Dates       string    `json:"dates"`
	ServiceType uint64    `json:"service_type"`
}

// Planning Center plan times, different times a plan has assigned.
type PlanTimes struct {
	ID           uint64    `gorm:"primary_key" json:"id"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Name         string    `json:"name"`
	TimeType     string    `json:"time_type"`
	StartsAt     time.Time `json:"starts_at"`
	EndsAt       time.Time `json:"ends_at"`
	LiveStartsAt time.Time `json:"live_starts_at"`
	LiveEndsAt   time.Time `json:"live_ends_at"`
	Plan         uint64    `json:"plan"`
}

// Planning Center people assigned to a plan.
type PlanPeople struct {
	ID               uint64    `gorm:"primary_key" json:"id"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	Status           string    `json:"status"`
	TeamPositionName string    `json:"team_position_name"`
	Person           uint64    `json:"person"`
	Plan             uint64    `json:"plan"`
}

// Planning Center people information.
type People struct {
	ID          uint64    `gorm:"primary_key" json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	ArchivedAt  time.Time `json:"archived_at"`
	Birthdate   time.Time `json:"birthdate"`
	Anniversary time.Time `json:"anniversary"`
	Status      string    `json:"status"`
	Permissions string    `json:"permissions"`
	FirstName   string    `json:"first_name"`
	LastName    string    `json:"last_name"`
	FacebookID  uint64    `json:"facebook_id"`
	Distance    uint64    `gorm:"-:all"`
}

// Slack users and their association with Planning Center people.
type SlackUsers struct {
	ID                string    `gorm:"primary_key" json:"id"`
	Name              string    `json:"name"`
	RealName          string    `json:"real_name"`
	FirstName         string    `json:"first_name"`
	LastName          string    `json:"last_name"`
	Email             string    `json:"email"`
	Phone             string    `json:"phone"`
	Deleted           bool      `json:"deleted"`
	IsBot             bool      `json:"is_bot"`
	IsAdmin           bool      `json:"is_admin"`
	IsOwner           bool      `json:"is_owner"`
	IsPrimaryOwner    bool      `json:"is_primary_owner"`
	IsRestricted      bool      `json:"is_restricted"`
	IsUltraRestricted bool      `json:"is_ultra_restricted"`
	IsStranger        bool      `json:"is_stranger"`
	IsAppUser         bool      `json:"is_app_user"`
	IsInvitedUser     bool      `json:"is_invited_user"`
	Updated           time.Time `json:"updated"`
	PCID              uint64    `json:"pc_id"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// Slack channels that were created and state information.
type SlackChannels struct {
	ID           string    `gorm:"primary_key" json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	PCPlan       uint64    `json:"pc_plan"`
	StartsAt     time.Time `json:"starts_at"`
	EndsAt       time.Time `json:"ends_at"`
	UsersInvited string    `json:"users_invited"`
	Archived     bool      `json:"archived"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Configure the database and add tables/adjust tables to match structures above.
func (a *App) InitDB() {
	var err error
	dbConfig := &gorm.Config{}
	// If debug is enabled, enable the logger.
	if a.config.DB.Debug {
		dbConfig.Logger = logger.Default.LogMode(logger.Info)
	}
	// Depending on connection configuration, open the database.
	if a.config.DB.Type == "sqlite3" {
		// Enable WAL journaling and a busy timeout. Without WAL, a single
		// long-running writer (e.g. the channel-creation/sync routine, which
		// interleaves slow Slack API calls with its writes) blocks all readers,
		// causing "database is locked" on concurrent reads such as the
		// send_message channel lookup. WAL lets reads proceed alongside the
		// writer, and the busy timeout makes any remaining contention wait
		// rather than fail immediately. Append as DSN pragmas, preserving any
		// query string already present in the configured connection.
		conn := a.config.DB.Connection
		sep := "?"
		if strings.Contains(conn, "?") {
			sep = "&"
		}
		conn += sep + "_journal_mode=WAL&_busy_timeout=10000"
		a.db, err = gorm.Open(sqlite.Open(conn), dbConfig)
	} else if a.config.DB.Type == "mysql" {
		a.db, err = gorm.Open(mysql.Open(a.config.DB.Connection), dbConfig)
	} else if a.config.DB.Type == "postgres" {
		a.db, err = gorm.Open(postgres.Open(a.config.DB.Connection), dbConfig)
	} else {
		log.Fatal("Incorrect database config")
	}
	// If a error occurs connecting to the database, fail.
	if err != nil {
		log.Fatal(err)
	}

	// Update tables on database to match the above definitions.
	a.db.AutoMigrate(&ServiceTypes{})
	a.db.AutoMigrate(&Plans{})
	a.db.AutoMigrate(&PlanTimes{})
	a.db.AutoMigrate(&PlanPeople{})
	a.db.AutoMigrate(&People{})
	a.db.AutoMigrate(&SlackUsers{})
	a.db.AutoMigrate(&SlackChannels{})
}
