package focus

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/pterm/pterm"
)

const (
	errParsingDate = Error(
		"The specified date format must be: YYYY-MM-DD or YYYY-MM-DD HH:MM:SS PM",
	)
	errInvalidDateRange = Error(
		"The end date must not be earlier than the start date",
	)
)

const (
	hoursInADay      = 24
	maxHoursInAMonth = 744  // 31 day months
	maxHoursInAYear  = 8784 // Leap years
	minutesInAnHour  = 60
)

const (
	barChartChar = "▇"
)

type timePeriod string

const (
	periodAllTime   timePeriod = "all-time"
	periodToday     timePeriod = "today"
	periodYesterday timePeriod = "yesterday"
	period7Days     timePeriod = "7days"
	period14Days    timePeriod = "14days"
	period30Days    timePeriod = "30days"
	period90Days    timePeriod = "90days"
	period180Days   timePeriod = "180days"
	period365Days   timePeriod = "365days"
)

var statsPeriod = []timePeriod{periodAllTime, periodToday, periodYesterday, period7Days, period14Days, period30Days, period90Days, period180Days, period365Days}

type quantity struct {
	minutes   int
	completed int
	abandoned int
}

// getPeriod returns the start and end time according to the
// specified time period.
func getPeriod(period timePeriod) (start, end time.Time) {
	now := time.Now()

	end = time.Date(
		now.Year(),
		now.Month(),
		now.Day(),
		23,
		59,
		59,
		0,
		now.Location(),
	)

	switch period {
	case periodToday:
		start = now
	case periodYesterday:
		start = now.AddDate(0, 0, -1)
		year, month, day := start.Date()
		end = time.Date(year, month, day, 23, 59, 59, 0, start.Location())
	case period7Days:
		start = now.AddDate(0, 0, -6)
	case period14Days:
		start = now.AddDate(0, 0, -13)
	case period30Days:
		start = now.AddDate(0, 0, -29)
	case period90Days:
		start = now.AddDate(0, 0, -89)
	case period180Days:
		start = now.AddDate(0, 0, -179)
	case period365Days:
		start = now.AddDate(0, 0, -364)
	case periodAllTime:
		return start, end
	default:
		return start, end
	}

	return time.Date(
		start.Year(),
		start.Month(),
		start.Day(),
		0,
		0,
		0,
		0,
		start.Location(),
	), end
}

// Data represents the computed statistics data
// for the current time period.
type Data struct {
	Weekday          map[time.Weekday]*quantity
	HourofDay        map[int]*quantity
	History          map[string]*quantity
	HistoryKeyFormat string
	Totals           quantity
	Averages         quantity
}

// initData creates an instance of Data with
// all its values initialised properly.
func initData(start, end time.Time, hoursDiff int) *Data {
	d := &Data{}

	d.Weekday = make(map[time.Weekday]*quantity)
	d.History = make(map[string]*quantity)
	d.HourofDay = make(map[int]*quantity)

	for i := 0; i <= 6; i++ {
		d.Weekday[time.Weekday(i)] = &quantity{}
	}

	for i := 0; i <= 23; i++ {
		d.HourofDay[i] = &quantity{}
	}

	// Decide whether to compute the work history
	// in terms of days, or months
	d.HistoryKeyFormat = "January 2006"
	if hoursDiff > hoursInADay && hoursDiff <= maxHoursInAMonth {
		d.HistoryKeyFormat = "January 02, 2006"
	} else if hoursDiff > maxHoursInAYear {
		d.HistoryKeyFormat = "2006"
	}

	for date := start; !date.After(end); date = date.Add(time.Duration(hoursInADay) * time.Hour) {
		d.History[date.Format(d.HistoryKeyFormat)] = &quantity{}
	}

	return d
}

// computeAverages calculates the average minutes, completed sessions,
// and abandoned sessions per day for the specified time period.
func (d *Data) computeAverages(start, end time.Time) {
	end = time.Date(
		end.Year(),
		end.Month(),
		end.Day(),
		23,
		59,
		59,
		0,
		end.Location(),
	)
	hoursDiff := roundTime(end.Sub(start).Hours())
	hoursInADay := 24

	numberOfDays := hoursDiff / hoursInADay

	d.Averages.minutes = roundTime(
		float64(d.Totals.minutes) / float64(numberOfDays),
	)
	d.Averages.completed = roundTime(
		float64(d.Totals.completed) / float64(numberOfDays),
	)
	d.Averages.abandoned = roundTime(
		float64(d.Totals.abandoned) / float64(numberOfDays),
	)
}

// calculateSessionDuration returns the session duration in seconds.
// It ensures that minutes that are not within the bounds of the
// reporting period, are not included.
func (d *Data) calculateSessionDuration(
	s *session,
	statsStart, statsEnd time.Time,
) float64 {
	var seconds float64

	hourly := map[int]float64{}
	weekday := map[time.Weekday]float64{}
	daily := map[string]float64{}

	for _, v := range s.Timeline {
		var durationAdded bool

		for date := v.StartTime; !date.After(v.EndTime); date = date.Add(1 * time.Minute) {
			// prevent minutes that fall outside the specified bounds
			// from being included
			if date.Before(statsStart) || date.After(statsEnd) {
				continue
			}

			var end time.Time
			if date.Add(1 * time.Minute).After(v.EndTime) {
				end = v.EndTime
			} else {
				end = date.Add(1 * time.Minute)
			}

			secs := end.Sub(date).Seconds()

			hourly[date.Hour()] += secs
			weekday[date.Weekday()] += secs
			daily[date.Format(d.HistoryKeyFormat)] += secs

			if !durationAdded {
				durationAdded = true
				seconds += v.EndTime.Sub(date).Seconds()
			}
		}
	}

	for k, val := range weekday {
		d.Weekday[k].minutes += roundTime(val / float64(minutesInAnHour))
	}

	for k, val := range hourly {
		d.HourofDay[k].minutes += roundTime(val / float64(minutesInAnHour))
	}

	for k, val := range daily {
		if _, exists := d.History[k]; exists {
			d.History[k].minutes += roundTime(val / float64(minutesInAnHour))
		}
	}

	return seconds
}

// computeTotals calculates the total minutes, completed sessions,
// and abandoned sessions for the current time period.
func (d *Data) computeTotals(sessions []session, startTime, endTime time.Time) {
	for i := range sessions {
		s := sessions[i]

		if s.EndTime.IsZero() {
			continue
		}

		duration := roundTime(
			d.calculateSessionDuration(
				&s,
				startTime,
				endTime,
			) / float64(
				minutesInAnHour,
			),
		)

		if s.Completed {
			d.Weekday[s.StartTime.Weekday()].completed++
			d.HourofDay[s.StartTime.Hour()].completed++

			if _, exists := d.History[s.StartTime.Format(d.HistoryKeyFormat)]; exists {
				d.History[s.StartTime.Format(d.HistoryKeyFormat)].completed++
			}

			d.Totals.completed++
			d.Totals.minutes += duration
		} else {
			d.Weekday[s.StartTime.Weekday()].abandoned++
			d.HourofDay[s.StartTime.Hour()].abandoned++

			if _, exists := d.History[s.StartTime.Format(d.HistoryKeyFormat)]; exists {
				d.History[s.StartTime.Format(d.HistoryKeyFormat)].abandoned++
			}

			d.Totals.abandoned++
			d.Totals.minutes += duration
		}
	}
}

// Stats represents the statistics for a time period.
type Stats struct {
	StartTime time.Time
	EndTime   time.Time
	Sessions  []session
	store     DB
	Data      *Data
	HoursDiff int
}

// getSessions retrieves the work sessions
// for the specified time period.
func (s *Stats) getSessions(start, end time.Time) error {
	b, err := s.store.getSessions(start, end)
	if err != nil {
		return err
	}

	for _, v := range b {
		sess := session{}

		err = json.Unmarshal(v, &sess)
		if err != nil {
			return err
		}

		s.Sessions = append(s.Sessions, sess)
	}

	return nil
}

// displayHourlyBreakdown prints the hourly breakdown
// for the current time period.
func (s *Stats) displayHourlyBreakdown(w io.Writer) {
	fmt.Fprintf(w, "\n%s", pterm.LightBlue("Hourly breakdown (minutes)"))

	type keyValue struct {
		key   int
		value *quantity
	}

	sl := make([]keyValue, 0, len(s.Data.HourofDay))
	for k, v := range s.Data.HourofDay {
		sl = append(sl, keyValue{k, v})
	}

	sort.SliceStable(sl, func(i, j int) bool {
		return sl[i].key < sl[j].key
	})

	var bars pterm.Bars

	for _, v := range sl {
		val := s.Data.HourofDay[v.key]

		d := time.Date(2000, 1, 1, v.key, 0, 0, 0, time.UTC)

		bars = append(bars, pterm.Bar{
			Label: d.Format("03:04 PM"),
			Value: val.minutes,
		})
	}

	chart, err := pterm.DefaultBarChart.WithHorizontalBarCharacter(barChartChar).
		WithHorizontal().
		WithShowValue().
		WithBars(bars).
		Srender()
	if err != nil {
		pterm.Error.Println(err)
		return
	}

	fmt.Fprintln(w, chart)
}

// displayWorkHistory prints the appropriate bar graph
// for the current time period.
func (s *Stats) displayWorkHistory(w io.Writer) {
	if s.Data.Totals.minutes == 0 {
		return
	}

	fmt.Fprintf(w, "\n%s", pterm.LightBlue("Work history (minutes)"))

	type keyValue struct {
		key   string
		value *quantity
	}

	sl := make([]keyValue, 0, len(s.Data.History))
	for k, v := range s.Data.History {
		sl = append(sl, keyValue{k, v})
	}

	sort.Slice(sl, func(i, j int) bool {
		iTime, err := time.Parse(s.Data.HistoryKeyFormat, sl[i].key)
		if err != nil {
			return true
		}

		jTime, err := time.Parse(s.Data.HistoryKeyFormat, sl[j].key)
		if err != nil {
			return true
		}

		return iTime.Before(jTime)
	})

	var bars pterm.Bars

	for _, v := range sl {
		val := s.Data.History[v.key]

		bars = append(bars, pterm.Bar{
			Label: v.key,
			Value: val.minutes,
		})
	}

	chart, err := pterm.DefaultBarChart.WithHorizontalBarCharacter(barChartChar).
		WithHorizontal().
		WithShowValue().
		WithBars(bars).
		Srender()
	if err != nil {
		pterm.Error.Println(err)
		return
	}

	fmt.Fprintln(w, chart)
}

// displayWeeklyBreakdown prints the weekly breakdown
// for the current time period.
func (s *Stats) displayWeeklyBreakdown(w io.Writer) {
	fmt.Fprintf(w, "\n%s", pterm.LightBlue("Weekly breakdown (minutes)"))

	type keyValue struct {
		key   time.Weekday
		value *quantity
	}

	sl := make([]keyValue, 0, len(s.Data.Weekday))
	for k, v := range s.Data.Weekday {
		sl = append(sl, keyValue{k, v})
	}

	sort.SliceStable(sl, func(i, j int) bool {
		return int(sl[i].key) < int(sl[j].key)
	})

	var bars pterm.Bars

	for _, v := range sl {
		val := s.Data.Weekday[v.key]

		bars = append(bars, pterm.Bar{
			Label: v.key.String(),
			Value: val.minutes,
		})
	}

	chart, err := pterm.DefaultBarChart.WithHorizontalBarCharacter(barChartChar).
		WithHorizontal().
		WithShowValue().
		WithBars(bars).
		Srender()
	if err != nil {
		pterm.Error.Println(err)
		return
	}

	fmt.Fprintln(w, chart)
}

func (s *Stats) displayAverages(w io.Writer) {
	hoursDiff := roundTime(s.EndTime.Sub(s.StartTime).Hours())

	if hoursDiff > hoursInADay {
		fmt.Fprintf(w, "\n%s\n", pterm.LightBlue("Averages"))

		hours, minutes := minsToHoursAndMins(s.Data.Averages.minutes)

		fmt.Fprintln(
			w,
			"Average time logged per day:",
			pterm.Green(hours),
			pterm.Green("hours"),
			pterm.Green(minutes),
			pterm.Green("minutes"),
		)
		fmt.Fprintln(
			w,
			"Completed sessions per day:",
			pterm.Green(s.Data.Averages.completed),
		)
		fmt.Fprintln(
			w,
			"Abandoned sessions per day:",
			pterm.Green(s.Data.Averages.abandoned),
		)
	}
}

func (s *Stats) displaySummary(w io.Writer) {
	fmt.Fprintf(w, "%s\n", pterm.LightBlue("Summary"))

	hours, minutes := minsToHoursAndMins(s.Data.Totals.minutes)

	fmt.Fprintf(w,
		"Total time logged: %s %s %s %s\n",
		pterm.Green(hours),
		pterm.Green("hours"),
		pterm.Green(minutes),
		pterm.Green("minutes"),
	)

	fmt.Fprintln(
		w,
		"Work sessions completed:",
		pterm.Green(s.Data.Totals.completed),
	)
	fmt.Fprintln(
		w,
		"Work sessions abandoned:",
		pterm.Green(s.Data.Totals.abandoned),
	)
}

func (s *Stats) compute() {
	s.Data.computeTotals(s.Sessions, s.StartTime, s.EndTime)
	s.Data.computeAverages(s.StartTime, s.EndTime)
}

func printTable(data [][]string, w io.Writer) {
	table := tablewriter.NewWriter(w)
	table.SetHeader([]string{"#", "Start date", "End date", "Status"})
	table.SetAutoWrapText(false)

	for _, v := range data {
		table.Append(v)
	}

	table.Render()
}

// Delete attempts to delete all sessions that fall
// in the specified time range. It requests for
// confirmation before proceeding with the permanent
// removal of the sessions from the database.
func (s *Stats) Delete(w io.Writer, r io.Reader) error {
	err := s.List(w)
	if err != nil {
		return err
	}

	if len(s.Sessions) == 0 {
		return nil
	}

	warning := pterm.Warning.Sprint(
		"The above sessions will be deleted permanently. Press ENTER to proceed",
	)
	fmt.Fprint(w, warning)

	reader := bufio.NewReader(r)

	_, _ = reader.ReadString('\n')

	return s.store.deleteSessions(s.StartTime, s.EndTime)
}

// List prints out a table of all the sessions that
// were created within the specified time range.
func (s *Stats) List(w io.Writer) error {
	err := s.getSessions(s.StartTime, s.EndTime)
	if err != nil {
		return err
	}

	if len(s.Sessions) == 0 {
		pterm.Info.Println("No sessions found for the specified time range")
		return nil
	}

	data := make([][]string, len(s.Sessions))

	for i, v := range s.Sessions {
		statusText := pterm.Green("completed")
		if !v.Completed {
			statusText = pterm.Red("abandoned")
		}

		endDate := v.EndTime.Format("Jan 02, 2006 03:04 PM")
		if v.EndTime.IsZero() {
			endDate = ""
		}

		sl := []string{
			fmt.Sprintf("%d", i+1),
			v.StartTime.Format("Jan 02, 2006 03:04 PM"),
			endDate,
			statusText,
		}

		data = append(data, sl)
	}

	printTable(data, w)

	return nil
}

// Show displays the relevant statistics for the
// set time period after making the necessary calculations.
func (s *Stats) Show(w io.Writer) error {
	defer s.store.close()

	err := s.getSessions(s.StartTime, s.EndTime)
	if err != nil {
		return err
	}

	if s.StartTime.IsZero() && len(s.Sessions) > 0 {
		fs := s.Sessions[0].StartTime
		s.StartTime = time.Date(
			fs.Year(),
			fs.Month(),
			fs.Day(),
			0,
			0,
			0,
			0,
			fs.Location(),
		)
	}

	diff := s.EndTime.Sub(s.StartTime)
	s.HoursDiff = int(diff.Hours())

	s.Data = initData(s.StartTime, s.EndTime, s.HoursDiff)

	s.compute()

	reportingStart := s.StartTime.Format("January 02, 2006")
	reportingEnd := s.EndTime.Format("January 02, 2006")
	timePeriod := "Reporting period: " + reportingStart + " - " + reportingEnd

	header := pterm.DefaultHeader.WithBackgroundStyle(pterm.NewStyle(pterm.BgYellow)).
		WithTextStyle(pterm.NewStyle(pterm.FgBlack)).
		Sprintfln(timePeriod)

	fmt.Fprint(w, header)

	s.displaySummary(w)
	s.displayAverages(w)

	if s.HoursDiff > hoursInADay {
		s.displayWorkHistory(w)
	}

	s.displayWeeklyBreakdown(w)

	s.displayHourlyBreakdown(w)

	return nil
}

type statsCtx interface {
	String(name string) string
}

// NewStats returns an instance of Stats constructed
// from command-line arguments.
func NewStats(ctx statsCtx, store DB) (*Stats, error) {
	s := &Stats{}
	s.store = store

	period := ctx.String("period")

	if period != "" && !contains(statsPeriod, timePeriod(period)) {
		var sl []string
		for _, v := range statsPeriod {
			sl = append(sl, string(v))
		}

		return nil, fmt.Errorf(
			"Period must be one of: %s",
			strings.Join(sl, ", "),
		)
	}

	s.StartTime, s.EndTime = getPeriod(timePeriod(period))

	// start and end options will override the set period
	start := strings.TrimSpace(ctx.String("start"))
	end := strings.TrimSpace(ctx.String("end"))

	timeFormatLength := 10 // for YYYY-MM-DD

	if start != "" {
		if len(start) == timeFormatLength {
			start += " 12:00:00 AM"
		}

		v, err := time.Parse("2006-1-2 3:4:5 PM", start)
		if err != nil {
			return nil, errParsingDate
		}

		// Using time.Date allows setting the correct time zone
		// instead of UTC time
		s.StartTime = time.Date(
			v.Year(),
			v.Month(),
			v.Day(),
			v.Hour(),
			v.Minute(),
			v.Second(),
			0,
			time.Now().Location(),
		)
	}

	if end != "" {
		if len(end) == timeFormatLength {
			end += " 11:59:59 PM"
		}

		v, err := time.Parse("2006-1-2 3:4:5 PM", end)
		if err != nil {
			return nil, errParsingDate
		}

		s.EndTime = time.Date(
			v.Year(),
			v.Month(),
			v.Day(),
			v.Hour(),
			v.Minute(),
			v.Second(),
			0,
			time.Now().Location(),
		)
	}

	if int(s.EndTime.Sub(s.StartTime).Seconds()) < 0 {
		return nil, errInvalidDateRange
	}

	return s, nil
}

// roundTime rounds a time value in seconds, minutes, or hours to the nearest integer.
func roundTime(t float64) int {
	return int(math.Round(t))
}

// minsToHoursAndMins expresses a minutes value
// in hours and mins.
func minsToHoursAndMins(val int) (hrs, mins int) {
	hrs = int(math.Floor(float64(val) / float64(minutesInAnHour)))
	mins = val % minutesInAnHour

	return
}

// contains checks if a string is present in
// a string slice.
func contains(s []timePeriod, e timePeriod) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}

	return false
}
