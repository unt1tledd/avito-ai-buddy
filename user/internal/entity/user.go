package entity

import "time"

type Profile struct {
	UserID     int64
	LastName   string
	FirstName  string
	MiddleName *string
}

type Team struct {
	ID   int64
	Name string
}

type Internship struct {
	TeamID         int64
	MentorUserID   *int64
	Track          string
	StartDate      time.Time
	PlannedEndDate time.Time
	ActualEndDate  *time.Time
}

type Metadata struct {
	InternshipID        int64
	Intern              Profile
	Team                Team
	Mentor              *Profile
	Internship          Internship
	PlannedDurationDays int32
	ActualDurationDays  *int32
}
