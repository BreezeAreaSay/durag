package telegram

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

// Fixture generated independently with Python's hmac module.
const (
	fixtureToken    = "7000000000:AAFakeTokenForUnitTests_1234567890abc"
	fixtureInitData = "query_id=AAHdF6IQAAAAAN0XohDhrOrc&user=%7B%22id%22%3A279058397%2C%22first_name%22%3A%22%D0%92%D0%BB%D0%B0%D0%B4%D0%B8%D1%81%D0%BB%D0%B0%D0%B2%22%2C%22last_name%22%3A%22Kibenko%22%2C%22username%22%3A%22vdkfrost%22%2C%22language_code%22%3A%22ru%22%2C%22is_premium%22%3Atrue%2C%22allows_write_to_pm%22%3Atrue%7D&auth_date=1700000000&start_param=ROOM42&hash=d671f951ee2a73e807086c84ba0d42554e014fc46cab47ed9f8a1b48d9ee2d6b"
	fixtureHash     = "d671f951ee2a73e807086c84ba0d42554e014fc46cab47ed9f8a1b48d9ee2d6b"
)

func TestValidateAcceptsGenuineInitData(t *testing.T) {
	d, err := validateAt(fixtureInitData, fixtureToken, 24*time.Hour, time.Unix(1700000000+3600, 0))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if d.Hash != fixtureHash {
		t.Fatalf("hash %s", d.Hash)
	}
	if d.User == nil || d.User.ID != 279058397 || d.User.FirstName != "Владислав" || d.User.Username != "vdkfrost" {
		t.Fatalf("user %+v", d.User)
	}
	if d.StartParam != "ROOM42" || d.QueryID != "AAHdF6IQAAAAAN0XohDhrOrc" {
		t.Fatalf("start_param %q query_id %q", d.StartParam, d.QueryID)
	}
	if d.AuthDate.Unix() != 1700000000 {
		t.Fatalf("auth date %v", d.AuthDate)
	}
	if d.DisplayName() != "Владислав" {
		t.Fatalf("display name %q", d.DisplayName())
	}
	// Validate() uses the wall clock; with no max age it must still pass.
	if _, err := Validate(fixtureInitData, fixtureToken, 0); err != nil {
		t.Fatalf("Validate without max age: %v", err)
	}
}

func TestValidateRejectsTamperedData(t *testing.T) {
	tampered := strings.Replace(fixtureInitData, "ROOM42", "ROOM43", 1)
	if _, err := validateAt(tampered, fixtureToken, 0, time.Now()); err != ErrInvalidSignature {
		t.Fatalf("tampered: %v", err)
	}
	if _, err := validateAt(fixtureInitData, "other:token", 0, time.Now()); err != ErrInvalidSignature {
		t.Fatalf("wrong token: %v", err)
	}
	if _, err := validateAt(strings.Replace(fixtureInitData, "hash="+fixtureHash, "hash=deadbeef", 1), fixtureToken, 0, time.Now()); err != ErrInvalidSignature {
		t.Fatalf("bad hash: %v", err)
	}
}

func TestValidateErrors(t *testing.T) {
	if _, err := validateAt("", fixtureToken, 0, time.Now()); err != ErrEmpty {
		t.Fatalf("empty: %v", err)
	}
	if _, err := validateAt("auth_date=1&user=%7B%7D", fixtureToken, 0, time.Now()); err != ErrMissingHash {
		t.Fatalf("missing hash: %v", err)
	}
	if _, err := validateAt(fixtureInitData, "", 0, time.Now()); err != ErrNoBotToken {
		t.Fatalf("no token: %v", err)
	}
	if _, err := validateAt(fixtureInitData, fixtureToken, time.Hour, time.Unix(1700000000+7200, 0)); err != ErrExpired {
		t.Fatalf("expired: %v", err)
	}
}

func TestSignRoundTrip(t *testing.T) {
	v := url.Values{}
	v.Set("auth_date", "1700000000")
	v.Set("user", `{"id":42,"first_name":"Ann"}`)
	v.Set("query_id", "q")
	token := "1:abc"
	v.Set("hash", Sign(v, token))
	d, err := validateAt(v.Encode(), token, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if d.User.ID != 42 || d.DisplayName() != "Ann" {
		t.Fatalf("%+v", d.User)
	}
	if DataCheckString(v) != "auth_date=1700000000\nquery_id=q\nuser={\"id\":42,\"first_name\":\"Ann\"}" {
		t.Fatalf("data check string %q", DataCheckString(v))
	}
}
