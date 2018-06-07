package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	ics "github.com/PuloV/ics-golang"
	toggl "github.com/jason0x43/go-toggl"
)

// Scheduler enters time entries into toggl based on .ics file
type Scheduler struct {
	token  string // api token
	path   string // path to .ics file
	hour   int    // time to update
	minute int
	second int
}

// begin scheduling. waits until first schedule time, then schedules, then waits
// 24 hours, then schedules, repeat...
func (sch *Scheduler) do() {

	// wait until next time to schedule, then schedule
	for {
		sch.beginScheduling()

		d := durUntilClock(sch.hour, sch.minute, sch.second)
		wait(d, "Until next scheduletime")
	}
}

func (sch *Scheduler) beginScheduling() {

	// get parser for the file we're working with
	parser := prepareParser(sch.path)

	// (try to) find out what lectures I had today
	events, err := lecturesAt(parser, time.Now())

	// if something went wrong, just give up... (continues running though)
	// TODO make it try again later
	if err != nil {
		fmt.Println("Failed to scheduleJobs: ", err.Error())
		return
	}

	// print today's events
	fmt.Println("Events for today:")
	for _, e := range events {
		fmt.Println("\t- ", e.GetDescription())
	}

	// open session and start entering the events
	session := toggl.OpenSession(sch.token)
	enterTimes(session, events)

	fmt.Println("DONE FOR TODAY")
}

// sleep, and enter time entries at the appropriate times
func enterTimes(session toggl.Session, events []*ics.Event) {

	// if there are no events, give up
	if len(events) == 0 {
		fmt.Println("no events")
		return
	}

	events = sortEvents(events)

	var err error          // error in toggl api calls
	var te toggl.TimeEntry // time entry returned from toggl in api calls
	const retryWait = 30   // time to wait until next retry

	// time until next toggl action
	diff := durUntilTime(events[0].GetStart())

	// for each event
	for i := range events {

		// sleep until start of event
		wait(diff, "Until start of "+events[i].GetDescription())

		projectID := getIDFromCode(events[i].GetSummary())

		// start time entry for event[0]. try a maximum of 3 times
		fmt.Println("START: ", events[i].GetDescription())
		for j := 0; j < 3; j++ {
			if projectID != 0 {
				te, err = session.StartTimeEntryForProject(events[i].GetDescription(),
					projectID, false)
			} else {
				fmt.Println("...Inserting time entry without project")
				te, err = session.StartTimeEntry(events[i].GetDescription())
			}

			if err == nil {
				break
			}

			fmt.Println("Coudln't start toggl entry: (", err.Error(),
				"), RETRYING ", j)
			wait(time.Second*retryWait, "until next retry")
		}

		// sleep until end of event
		diff = durUntilTime(events[i].GetEnd())
		wait(diff, "Until end of "+events[i].GetDescription())

		// stop the started time entry. try a maximum of 3 times
		fmt.Println("\nEND: ", events[i].GetDescription())
		for j := 0; j < 3; j++ {
			te, err = session.StopTimeEntry(te)
			if err == nil {
				break
			}

			fmt.Println("Coudln't stop toggl entry: (", err.Error(),
				"), RETRYING ", j)
			wait(time.Second*retryWait, "until next retry")
		}

		// if this isn't the last event, calculate diff until start of event
		if i < len(events)-1 {
			diff = durUntilTime(events[i+1].GetStart())
		}
	}
}

// determine toggl id from IMT code found in ics file
func getIDFromCode(s string) int {
	switch s {
	case "IMT1362": // UX
		return 62056917
	case "IMT2021": // algmet
		return 62056509
	case "IMT2571": // databaser
		return 62056716
	case "IMT2681": // cloud
		return 61803225
	case "IMT3003": // tjenestearkitektur og drift
		return 93287263
	case "IMT2282": // os
		return 92487328
	case "IMT2291": // www
		return 92526098
	case "IMT3673": // mobile
		return 93349063

	default:
		return 0
	}
}

// Get lectures at particular time as ics events
func lecturesAt(parser *ics.Parser, when time.Time) ([]*ics.Event, error) {

	/*
		events := make([]*ics.Event, 3)

		events[0] = &ics.Event{}
		events[1] = &ics.Event{}
		events[2] = &ics.Event{}

		// set start, end (fake, rapid)
		now := time.Now()

		events[0].SetStart(now.Add(time.Second * 1))
		events[0].SetEnd(now.Add(time.Second * 10))
		events[0].SetDescription("Forelesning i ..")
		events[0].SetSummary("IMT1362")

		events[1].SetStart(now.Add(time.Second * 20))
		events[1].SetEnd(now.Add(time.Second * 30))
		events[1].SetDescription("Forelesning i nokke anna")
		events[1].SetSummary("IMT2021")

		events[2].SetStart(now.Add(time.Second * 40))
		events[2].SetEnd(now.Add(time.Second * 50))
		events[2].SetDescription("Forelesning i rare greier")
		events[2].SetSummary("IMT2681")

		return events, nil
	*/

	// get all calendars from parser
	cals, errCals := parser.GetCalendars()

	// if error or no calendars, error
	if errCals != nil {
		return nil, errCals
	} else if len(cals) == 0 {
		return nil, errors.New("No calendars (needed one)")
	}

	// get events for time 'when' (using first calendar)
	eventsForDay, errEvents := cals[0].GetEventsByDate(when)

	if errEvents != nil { // error -> error
		return nil, errEvents
	}

	// Filter out events that don't start with "Forelesning"
	// (To prevent toggl trakcking labs I'm not in) TODO: change?
	events := make([]*ics.Event, 0)
	for _, e := range eventsForDay {
		if strings.HasPrefix(e.GetDescription(), "Forelesning") {
			events = append(events, e)
			fmt.Println(when, " - ", e)
		}
	}
	return events, nil
}

// return parser ready to parse ics file pointed to by path
func prepareParser(path string) *ics.Parser {
	parser := ics.New()
	inputChan := parser.GetInputChan()
	inputChan <- path
	parser.Wait()

	cals, _ := parser.GetCalendars()
	str := cals[0].GetDesc()
	fmt.Println(str)

	return parser
}

// print formated message -> sleep d
func wait(d time.Duration, msg string) {
	fmt.Println("Sleeping (", d, "): ", msg)
	time.Sleep(d)
}

// sorts the events in ascheding order according to their start times
// (using selection sort)
func sortEvents(events []*ics.Event) []*ics.Event {

	sorted := events

	for i := 0; i < len(events)-1; i++ {

		lowest := i
		for j := i + 1; j < len(events); j++ {
			if sorted[j].GetStart().Unix() < sorted[lowest].GetStart().Unix() {
				lowest = j
			}
		}

		if lowest != i {
			temp := events[i]
			events[i] = events[lowest]
			events[lowest] = temp
		}
	}

	return sorted
}

// calculate duration until next HH:MM:SS
func durUntilClock(hour, minute, second int) time.Duration {
	t := time.Now()

	// the time this HH:MM:SS is happening
	when := time.Date(t.Year(), t.Month(), t.Day(), hour,
		minute, second, 0, t.Location())

	// d is the time until next such time
	d := when.Sub(t)

	// if duration is negative, add a day
	if d < 0 {
		when = when.Add(24 * time.Hour)
		d = when.Sub(t)
	}

	return d
}

// calculate duration until time is when
func durUntilTime(when time.Time) time.Duration {
	return when.Sub(time.Now())
}
