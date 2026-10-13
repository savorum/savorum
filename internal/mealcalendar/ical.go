// Package mealcalendar publishes the meal plan as iCalendar (RFC 5545).
package mealcalendar

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/savorum/savorum/internal/meal"
)

// Calendar describes the document around the planned meals.
type Calendar struct {
	Name     string
	Instance string // namespaces event UIDs
	PlanURL  string // optional link back to the plan
	Stamp    time.Time
}

var mealTypeLabels = map[meal.MealType]string{
	meal.MealTypeBreakfast: "Breakfast",
	meal.MealTypeLunch:     "Lunch",
	meal.MealTypeDinner:    "Dinner",
	meal.MealTypeSnack:     "Snack",
}

// Encode writes dated meals as all-day events; skipped meals are cancelled.
func Encode(calendar Calendar, meals []meal.Meal) []byte {
	var out strings.Builder

	line := func(value string) { writeFolded(&out, value) }

	line("BEGIN:VCALENDAR")
	line("VERSION:2.0")
	line("PRODID:-//Savorum//Meal plan//EN")
	line("CALSCALE:GREGORIAN")
	line("METHOD:PUBLISH")

	if calendar.Name != "" {
		line("X-WR-CALNAME:" + escapeText(calendar.Name))
	}

	line("REFRESH-INTERVAL;VALUE=DURATION:PT1H")
	line("X-PUBLISHED-TTL:PT1H")

	stamp := calendar.Stamp.UTC().Format("20060102T150405Z")

	for _, planned := range meals {
		if planned.Date == nil {
			continue
		}

		day, err := time.Parse(time.DateOnly, planned.Date.String())
		if err != nil {
			continue
		}

		line("BEGIN:VEVENT")
		line("UID:meal-" + planned.ID + "@" + calendar.Instance)
		line("DTSTAMP:" + stamp)
		line("DTSTART;VALUE=DATE:" + day.Format("20060102"))
		line("DTEND;VALUE=DATE:" + day.AddDate(0, 0, 1).Format("20060102"))
		line("SUMMARY:" + escapeText(summary(planned)))

		if description := describe(planned); description != "" {
			line("DESCRIPTION:" + escapeText(description))
		}

		if calendar.PlanURL != "" {
			line("URL:" + calendar.PlanURL + "?date=" + planned.Date.String())
		}

		line(fmt.Sprintf("SEQUENCE:%d", max(planned.Revision, 1)-1))

		if !planned.UpdatedAt.IsZero() {
			line("LAST-MODIFIED:" + planned.UpdatedAt.UTC().Format("20060102T150405Z"))
		}

		if planned.Skipped {
			line("STATUS:CANCELLED")
		} else {
			line("STATUS:CONFIRMED")
		}

		line("TRANSP:TRANSPARENT")
		line("END:VEVENT")
	}

	line("END:VCALENDAR")

	return []byte(out.String())
}

func summary(planned meal.Meal) string {
	title := planned.DisplayTitle()
	if title == "" {
		title = "Planned meal"
	}

	if label, ok := mealTypeLabels[planned.MealType]; ok {
		return label + ": " + title
	}

	return title
}

// describe omits meal notes: anyone holding a feed URL can read it.
func describe(planned meal.Meal) string {
	lines := []string{}
	informative := planned.Name != "" || len(planned.Dishes) > 1

	for _, dish := range planned.Dishes {
		text := dish.DisplayName
		if dish.Scale != nil && *dish.Scale != "1" && *dish.Scale != "" {
			text += " (×" + dish.Scale.String() + ")"
			informative = true
		}

		if dish.LeftoverOf != "" {
			text += " (leftovers)"
			informative = true
		}

		lines = append(lines, text)
	}

	if !informative {
		return ""
	}

	return strings.Join(lines, "\n")
}

func escapeText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	replacer := strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\n", `\n`, "\r", `\n`)

	return replacer.Replace(value)
}

// writeFolded folds at 75 octets without splitting a UTF-8 sequence.
func writeFolded(out *strings.Builder, value string) {
	const limit = 75

	width := 0

	for index, r := range value {
		size := utf8.RuneLen(r)
		if width+size > limit {
			out.WriteString("\r\n ")

			width = 1
		}

		out.WriteString(value[index : index+size])
		width += size
	}

	out.WriteString("\r\n")
}
