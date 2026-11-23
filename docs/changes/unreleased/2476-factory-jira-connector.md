---
kind: added
pr: 2476
title: The factory reads Jira Cloud tickets as work and comments the pull request back
surface: [engine, app, factory]
invalidates:
  - "The factory took work only from GitHub issues. factory.jira_connect {factory, site, email, token, jql} now connects this machine's factory to a Jira Cloud site (checked against /myself and one search before anything is kept); every ticket the JQL finds is polled once a minute into a job through the inbox, carrying a jira-project:<KEY> label for routing and Jira's priority as p0-p3. The ticket gets a comment with the pull request's link when it opens and when it merges. factory.jira_status and factory.jira_disconnect read and forget the connection, and hello announces factory.jira."
  - "The Jira API token is kept like the GitHub token (the app's keys in app mode, else the profile's jira_token), never in the factory's folder and never in an answer. Not connected, nothing reads Jira: no loop starts."
---
