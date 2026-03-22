package main

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/agnivade/levenshtein"
	"github.com/slack-go/slack"
)

// Update planning center database tables with data from PC API.
func UpdatePCData() {
	// We don't need to update the archive if we already have data from the past,
	// as such we get the current time and see if we already have an entry in the future.
	// Its possible that some people only schedule services once a week, so we check with
	// the current date subtracted by 14 days.
	var updateFrom time.Time
	now := time.Now().UTC()
	var futurePlan Plans
	app.db.Where("first_time_at >= ?", now.Add(time.Hour*24*14*-1)).Order("first_time_at ASC").First(&futurePlan)
	if futurePlan.ID != 0 {
		// If a future plan exists, we update from past 30 days.
		updateFrom = now.Add(time.Hour * 24 * 30 * -1)
	}

	// Get all people.
	allPeople, err := PCGetAll("/services/v2/people")
	if err != nil {
		log.Fatalln(err)
	}
	// For each person, parse data and save to database.
	for _, data := range allPeople {
		// Parse the ID and attributes.
		id := data.GetUint64("id")
		attributes := data.GetDict("attributes")

		// Check if this person is already in our database.
		var p People
		app.db.Where("id = ?", id).First(&p)

		// Update all fields with new data.
		p.UpdatedAt = attributes.GetDate("updated_at")
		p.ArchivedAt = attributes.GetDate("archived_at")
		p.Birthdate = attributes.GetDate("birthdate")
		p.Anniversary = attributes.GetDate("anniversary")
		p.Status = attributes.GetString("status")
		p.Permissions = attributes.GetString("permissions")
		p.FirstName = attributes.GetString("first_name")
		p.LastName = attributes.GetString("last_name")
		p.FacebookID = attributes.GetUint64("facebook_id")

		// If the person wasn't in the database, create it.
		if p.ID == 0 {
			p.ID = id
			p.CreatedAt = attributes.GetDate("created_at")
			app.db.Create(&p)
		} else {
			// If th e person was in the database, update it.
			app.db.Save(&p)
		}
	}

	// Get service types.
	allServiceTypes, err := PCGetAll("/services/v2/service_types")
	if err != nil {
		log.Fatalln(err)
	}
	// Keep track of service type IDs incase no filter is supplied.
	var allServiceTypeIDs []uint64
	// For each service type, parse data and save to database.
	for _, data := range allServiceTypes {
		// Get the service ID and attributes.
		id := data.GetUint64("id")
		allServiceTypeIDs = append(allServiceTypeIDs, id)
		attributes := data.GetDict("attributes")

		// Check if service type was already in database.
		var s ServiceTypes
		app.db.Where("id = ?", id).First(&s)

		// Update fields with new data.
		s.UpdatedAt = attributes.GetDate("updated_at")
		s.ArchivedAt = attributes.GetDate("archived_at")
		s.DeletedAt = attributes.GetDate("deleted_at")
		s.Name = attributes.GetString("name")

		// If service type wasn't already existing, create it.
		if s.ID == 0 {
			s.ID = id
			s.CreatedAt = attributes.GetDate("created_at")
			app.db.Create(&s)
		} else {
			// Save if already existing/
			app.db.Save(&s)
		}
	}

	// Get service type filter from the config.
	servicesTypesToPull := app.config.PlanningCenter.ServiceTypeIDs
	// If no filter, use the found service types above.
	if len(servicesTypesToPull) == 0 {
		servicesTypesToPull = allServiceTypeIDs
	}

	// For each service type, pull plans and plan info.
	for _, serviceTypeID := range servicesTypesToPull {
		// Get the plans for this service type.
		allPlans, err := PCGetAll(fmt.Sprintf("/services/v2/service_types/%d/plans", serviceTypeID))
		if err != nil {
			log.Println("Error getting plans for service type:", serviceTypeID, err)
			continue
		}
		// For each plan, update data in database and pull other plan releated items for updates.
		for _, data := range allPlans {
			// Get the plan ID and attributes.
			planID := data.GetUint64("id")
			attributes := data.GetDict("attributes")

			// Check if plan was already in the database.
			var p Plans
			app.db.Where("id = ?", planID).First(&p)

			// Update with new data.
			p.UpdatedAt = attributes.GetDate("updated_at")
			p.SeriesTitle = attributes.GetString("series_title")
			p.Title = attributes.GetString("title")
			p.FirstTimeAt = attributes.GetDate("sort_date")
			p.LastTimeAt = attributes.GetDate("last_time_at")
			p.MultiDay = attributes.GetBool("multi_day")
			p.Dates = attributes.GetString("dates")

			// If either updated at or first time at for the plan is before the update from date,
			// we can process the update of data. Otherwise, we ignore this service as we do not care
			// about updating historic data. Updating historic data causes more API traffic than needed.
			if p.UpdatedAt.Before(updateFrom) && p.FirstTimeAt.Before(updateFrom) {
				continue
			}

			// If plan wasn't already created, create it.
			if p.ID == 0 {
				p.ID = planID
				p.CreatedAt = attributes.GetDate("created_at")
				p.ServiceType = serviceTypeID
				app.db.Create(&p)
			} else {
				// Save plan if already existing.
				app.db.Save(&p)
			}

			// Get all times for this plan.
			allPlanTimes, err := PCGetAll(fmt.Sprintf("/services/v2/service_types/%d/plans/%d/plan_times", serviceTypeID, planID))
			if err != nil {
				log.Println("Error getting plan times for plan:", planID, err)
				continue
			}
			// With each time, save it to the database.
			for _, data := range allPlanTimes {
				// Get the plan time ID and attributes.
				id := data.GetUint64("id")
				attributes := data.GetDict("attributes")

				// Get from database if already existing.
				var p PlanTimes
				app.db.Where("id = ?", id).First(&p)

				// Update data.
				p.UpdatedAt = attributes.GetDate("updated_at")
				p.Name = attributes.GetString("name")
				p.TimeType = attributes.GetString("time_type")
				p.StartsAt = attributes.GetDate("starts_at")
				p.EndsAt = attributes.GetDate("ends_at")
				p.LiveStartsAt = attributes.GetDate("live_starts_at")
				p.LiveEndsAt = attributes.GetDate("live_ends_at")

				// If not already existing, create it.
				if p.ID == 0 {
					p.ID = id
					p.CreatedAt = attributes.GetDate("created_at")
					p.Plan = planID
					app.db.Create(&p)
				} else {
					// If already existing, save it.
					app.db.Save(&p)
				}
			}

			// Get all members of the plan.
			allTeamMembers, err := PCGetAll(fmt.Sprintf("/services/v2/service_types/%d/plans/%d/team_members", serviceTypeID, planID))
			if err != nil {
				log.Println("Error getting team members for plan:", planID, err)
				continue
			}
			// With each member, update the database.
			for _, data := range allTeamMembers {
				// Get the member ID and attributes.
				id := data.GetUint64("id")
				attributes := data.GetDict("attributes")

				// Get person data from the database.
				var p PlanPeople
				app.db.Where("id = ?", id).First(&p)

				// Update data.
				p.UpdatedAt = attributes.GetDate("updated_at")
				p.Status = attributes.GetString("status")
				p.TeamPositionName = attributes.GetString("team_position_name")

				// If person wasn't existing, create them.
				if p.ID == 0 {
					p.ID = id
					p.CreatedAt = attributes.GetDate("created_at")
					p.Person = data.GetDict("relationships").GetDict("person").GetDict("data").GetUint64("id")
					p.Plan = planID
					app.db.Create(&p)
				} else {
					// Otherwise save new info.
					app.db.Save(&p)
				}
			}
		}
	}
}

// Update slack information.
func UpdateSlackData() {
	// Get all users from Slack.
	users, err := app.slack.GetUsers()
	if err != nil {
		log.Println("Error getting Slack users:", err)
		return
	}
	// If no users returned, error as we should have some...
	if len(users) == 0 {
		log.Println("No users found in Slack.")
		return
	}
	// With each user, update the database.
	for _, user := range users {
		// Check if user already is in database.
		var u SlackUsers
		app.db.Where("id = ?", user.ID).First(&u)

		// Update data.
		u.Name = user.Name
		u.RealName = user.RealName
		u.FirstName = user.Profile.FirstName
		u.LastName = user.Profile.LastName
		u.Email = user.Profile.Email
		u.Phone = user.Profile.Phone
		u.Deleted = user.Deleted
		u.IsBot = user.IsBot
		u.IsAdmin = user.IsAdmin
		u.IsOwner = user.IsOwner
		u.IsPrimaryOwner = user.IsPrimaryOwner
		u.IsRestricted = user.IsRestricted
		u.IsUltraRestricted = user.IsUltraRestricted
		u.IsStranger = user.IsStranger
		u.IsAppUser = user.IsAppUser
		u.IsInvitedUser = user.IsInvitedUser
		u.Updated = user.Updated.Time()

		// Try and find a match for this Slack user to the Planning Center people.
		var people []People
		// Get all people from Planning Center.
		app.db.Find(&people)
		if len(people) != 0 {
			// For each person, compute how close of a match they are to the Slack user.
			for i, person := range people {
				distance := levenshtein.ComputeDistance(u.Name, person.FirstName+" "+person.LastName)
				newDistance := levenshtein.ComputeDistance(u.RealName, person.FirstName+" "+person.LastName)
				// The lowest score of the first+lastname match is used.
				if newDistance < distance {
					distance = newDistance
				}
				// Compute a score of first+last name.
				newDistance = levenshtein.ComputeDistance(u.FirstName, person.FirstName)
				newDistance += levenshtein.ComputeDistance(u.LastName, person.LastName)
				// If this score is lower than the last score, return it.
				if newDistance < distance {
					distance = newDistance
				}
				// Update the distance on the user for sorting.
				people[i].Distance = uint64(distance)
			}

			// Sort all Planning Center people by the score computed.
			sort.Slice(people, func(i, j int) bool {
				return people[i].Distance < people[j].Distance
			})

			// Debug output for comparing scores.
			// for _, person := range people {
			// 	fmt.Printf("%d %s (%s) %s\n", person.Distance, u.Name, u.RealName, person.FirstName+" "+person.LastName)
			// }

			// Set the planning center ID to nothing at first.
			u.PCID = 0
			// If score of the first person is less than 7,
			// consider them a match and assign thier ID to the slack user.
			if people[0].Distance < 7 {
				u.PCID = people[0].ID
			}
		}

		// If not already existing in the database, create them.
		if u.ID == "" {
			u.ID = user.ID
			app.db.Create(&u)
		} else {
			// if already existing, update the user.
			app.db.Save(&u)
		}
	}
}

/*

Delay on channel descript/topic may not be long enough.

*/

// Create slack channels for upcoming services.
func CreateSlackChannels() {
	// Start at now.
	now := time.Now().UTC()
	startDate := now
	// If create from weekday is a valid weekday, attempt to turn back the clock to the
	// most recently past weekday. Use that day as the stating point so we do not
	// create channels in the future past the date we expect to have channels.
	// This is useful if you want to run the cron every day to keep channel title
	// and members up to date, but only want so many channels ahead of a certain weekday.
	if app.config.Slack.CreateFromWeekday != -1 && app.config.Slack.CreateFromWeekday <= 6 {
		// Get the current weekday and set the days to subtract to 0.
		thisWeekday := int(now.Weekday())
		var daysSub int = 0

		// If this weekday is the day we intend to create from, or if the weekday is
		// after. We want to just subtract this weekday from create form weekday which
		// should get us back to the most recent weekday.
		if thisWeekday >= app.config.Slack.CreateFromWeekday {
			daysSub = app.config.Slack.CreateFromWeekday - thisWeekday
		} else {
			// Otherwise, we have started a new week from that weekday and we need to
			// add 7 days to the current weekday in our subtraction. This will bring us
			// not to the next weekday, but the past weekday.
			daysSub = app.config.Slack.CreateFromWeekday - (thisWeekday + 7)
		}
		// Subtract the number of days calculated to bring us to the weekday to create form.
		startDate = now.Add(time.Hour * 24 * time.Duration(daysSub))
	}
	// Last date is start date plus duration of create channels ahead.
	lastDate := startDate.Add(app.config.Slack.CreateChannelsAhead)

	// Get plan times that match.
	var planTimes []PlanTimes
	app.db.Where("time_type='service' AND starts_at > ? AND starts_at < ?", startDate, lastDate).Find(&planTimes)
	// If no plan times matched, exit here.
	if len(planTimes) == 0 {
		log.Println("No services found for this time frame.")
		return
	}

	// With each plan time found, create a slack channel.
	for _, planTime := range planTimes {
		// Get the plan associated with the plan time.
		var plan Plans
		app.db.Where("id = ?", planTime.Plan).First(&plan)
		if plan.ID == 0 {
			log.Println("Unable to find plan:", planTime.Plan)
			continue
		}

		// Get the service type associated with the plan.
		var serviceType ServiceTypes
		app.db.Where("id = ?", plan.ServiceType).First(&serviceType)
		if serviceType.ID == 0 {
			log.Println("Unable to find service type:", planTime.Plan)
			continue
		}

		// Find people assigned to the plan.
		var peopleOnPlan []PlanPeople
		app.db.Where("plan = ?", plan.ID).Find(&peopleOnPlan)
		if len(peopleOnPlan) == 0 {
			log.Println("No people assigned to plan:", planTime.Plan)
			continue
		}

		// Check if a channel was already created for this plan.
		var channel SlackChannels
		app.db.Where("pc_plan = ?", plan.ID).First(&channel)

		// Set the topic/description based on servie type, and title/series title.
		topic := serviceType.Name
		if plan.SeriesTitle == "" && plan.Title != "" {
			topic = topic + " - " + plan.Title
		} else if plan.SeriesTitle != "" && plan.Title != "" {
			topic = topic + " - " + plan.SeriesTitle + " (" + plan.Title + ")"
		} else if plan.SeriesTitle != "" {
			topic = topic + " - " + plan.SeriesTitle
		}

		// If the channel already exists, we do not need to create it...
		// However, we should check if the description is changed
		// and we should check if people were added.
		if channel.ID != "" {
			if channel.Description != topic {
				app.slack.SetTopicOfConversation(channel.ID, topic)
				app.slack.SetPurposeOfConversation(channel.ID, topic)
				channel.Description = topic
				app.db.Save(&channel)
			}
		} else {
			// If the channel is being created, set the name to the starts at date.
			channel.Name = planTime.StartsAt.Format("2006-01-02")
			// Its possible that a duplicate channel already exists, if so we should append
			// a channel number. Duplicate channels typically happen if multiple plans
			// exists on the same day.
			startingID := 1
			for {
				var duplicateChannel SlackChannels
				app.db.Where("name = ?", channel.Name).First(&duplicateChannel)
				if duplicateChannel.ID == "" {
					break
				}
				startingID++
				channel.Name = fmt.Sprintf("%s_%d", planTime.StartsAt.Format("2006-01-02"), startingID)
			}

			// Create the channel.
			channelInfo := slack.CreateConversationParams{
				ChannelName: channel.Name,
				IsPrivate:   true,
			}
			log.Println("Creating channel:", channel.Name)
			schan, err := app.slack.CreateConversation(channelInfo)
			if err != nil {
				log.Println("Failed to create channel:", err)
				continue
			}

			// If topic is defined, set the topic and purpose.
			if topic != "" {
				// Keep count of failures so we can try again.
				failed := 0
				_, err = app.slack.SetTopicOfConversation(schan.ID, topic)
				if err != nil {
					failed++
					log.Println("Failed to set topic:", err)
				}
				_, err = app.slack.SetPurposeOfConversation(schan.ID, topic)
				if err != nil {
					failed++
					log.Println("Failed to set purpose:", err)
				}
				// If it failed, make topic empty so we can try again next run.
				if failed != 0 {
					topic = ""
				}
			}

			// Save the channel to the database.
			channel.ID = schan.ID
			channel.PCPlan = planTime.Plan
			channel.StartsAt = planTime.StartsAt
			channel.EndsAt = planTime.EndsAt
			channel.Description = topic
			app.db.Create(&channel)
		}

		// Get the previous users that were invited to the channel.
		invited := strings.Split(channel.UsersInvited, ",")
		// If nothing is previous, reset the slice to nil.
		if len(invited) == 1 && invited[0] == "" {
			invited = nil
		}

		// Keep a list of users we need to invite as they are new.
		var usersToInvite []string

		// For each sticky user, invite them.
		for _, stickyUser := range app.config.Slack.StickyUsers {
			// Check if they were already invited.
			alreadyInvited := false
			for _, uid := range invited {
				if uid == stickyUser {
					alreadyInvited = true
					break
				}
			}

			// Make sure they were not already added to the list of users.
			for _, uid := range usersToInvite {
				if uid == stickyUser {
					alreadyInvited = true
					break
				}
			}

			// If not already invited, add to the list of users to invite.
			if !alreadyInvited {
				usersToInvite = append(usersToInvite, stickyUser)
			}
		}

		// For each person on the plan, see if we need to invite them.
		for _, personOnPlan := range peopleOnPlan {
			// Find the slack user for the planning center person.
			var slackUser SlackUsers
			app.db.Where("pc_id = ?", personOnPlan.Person).First(&slackUser)
			if slackUser.ID == "" {
				continue
			}

			// Check if they were already invited.
			alreadyInvited := false
			for _, uid := range invited {
				if uid == slackUser.ID {
					alreadyInvited = true
					break
				}
			}

			// Make sure they were not already added to the list of users.
			// A person can be assigned to multiple teams on a plan.
			for _, uid := range usersToInvite {
				if uid == slackUser.ID {
					alreadyInvited = true
					break
				}
			}

			// If not already invited, add to the list of users to invite.
			if !alreadyInvited {
				usersToInvite = append(usersToInvite, slackUser.ID)
			}
		}

		// If there are users to invite, invite them.
		if len(usersToInvite) != 0 {
			// Update the invited users list.
			invited = append(invited, usersToInvite...)
			channel.UsersInvited = strings.Join(invited, ",")
			// Invite the users.
			_, err := app.slack.InviteUsersToConversation(channel.ID, usersToInvite...)
			if err != nil {
				log.Println("Failed to invite users to channel:", err)
			}
			// Update the channel on database with the new list of users invited.
			app.db.Save(&channel)
		}
	}

	// Find old channels to archive. Any channel which start at date is before the start date.
	var channelsToArchive []SlackChannels
	app.db.Where("starts_at < ? AND archived != 1", startDate).Find(&channelsToArchive)
	// Archive channels which are old.
	for _, channel := range channelsToArchive {
		err := app.slack.ArchiveConversation(channel.ID)
		if err != nil {
			log.Println("Error closing old channel:", err)
		}
		// Mark as archived on the database.
		channel.Archived = true
		app.db.Save(&channel)
	}
}
