package app

import (
	"bytes"
	"log"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestInitialAdministrator(t *testing.T) {
	for _, supplied := range []string{"", "provided-password"} {
		t.Run(map[bool]string{true: "generated", false: "provided"}[supplied == ""], func(t *testing.T) {
			var logs bytes.Buffer
			old := log.Writer()
			log.SetOutput(&logs)
			defer log.SetOutput(old)
			dir := t.TempDir()
			a, err := New(Config{DataDir: dir, InitializeAdmin: true, AdminUsername: "owner", AdminPassword: supplied})
			if err != nil {
				t.Fatal(err)
			}
			var hash string
			if err := a.DB.QueryRow(`SELECT password FROM users WHERE username='owner'`).Scan(&hash); err != nil {
				t.Fatal(err)
			}
			password := supplied
			if supplied == "" {
				match := regexp.MustCompile(`initial administrator: username="owner" password=(\S+)`).FindStringSubmatch(logs.String())
				if len(match) != 2 {
					t.Fatal("generated password missing from startup log")
				}
				password = match[1]
				if len(password) != 12 {
					t.Fatal("password must have 12 characters")
				}
				for _, pattern := range []string{`[a-z]`, `[A-Z]`, `[0-9]`, `[^a-zA-Z0-9]`} {
					if !regexp.MustCompile(pattern).MatchString(password) {
						t.Fatalf("missing character class %s", pattern)
					}
				}
			} else if strings.Contains(logs.String(), supplied) {
				t.Fatal("supplied password leaked into logs")
			}
			if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
				t.Fatal("initial password does not match stored hash")
			}
			a.Close()
			logs.Reset()
			a, err = New(Config{DataDir: dir, InitializeAdmin: true, AdminPassword: "replacement-password"})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			var after string
			if err := a.DB.QueryRow(`SELECT password FROM users WHERE username='owner'`).Scan(&after); err != nil {
				t.Fatal(err)
			}
			if after != hash {
				t.Fatal("restart changed password")
			}
			if logs.Len() != 0 {
				t.Fatal("restart logged credentials")
			}
		})
	}
}

func TestMigrationDoesNotInitializeAdministrator(t *testing.T) {
	a, err := New(Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var count int
	if err := a.DB.QueryRow(`SELECT count(*) FROM users`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("migration database must start without users")
	}
}
