# Recording meetings on the desk

## Record system audio and the microphone for a meeting

Open **Meeting notes** in the desk title strip. Enter a title, choose whether to
**Keep audio with this meeting**, then choose **Start recording**. In Linux the
desk records the default PipeWire output monitor and default microphone.
On macOS 15 or later it asks for Microphone and Screen & System Audio Recording
access. Windows capture is pending.

**Recording** is visible in the title strip even when the notes panel is closed.
Click **Stop** there once to stop both tracks. **Copy recording notice** copies a
short message you can share with people in the meeting. A starting Calendar
event mirrored to the local app data store with conferencing can offer **Meeting starting**. Opening that prompt
shows the capture controls. Only **Start recording** opens the audio devices.

## Live transcription, speaker labels, and local audio

The first recording downloads the local Whisper model automatically. It reads
short audio chunks on this computer. The output and microphone tracks carry
**Speaker 1** and **Speaker 2** labels. Multiple voices on one track can share a
label. If you keep audio, both WAV tracks stay in the private profile's
`meetings/audio` folder. Otherwise audio is held in memory while it is transcribed.

**Allow cloud transcription if local transcription fails** is off by default.
When selected, the engine can send an audio chunk to the configured voice model
only if the destination passes the profile's PII policy. Outside app harnesses
cannot use the capture methods.

## Live notes, action items, and follow-up drafts after recording

Select **Use my configured model for notes** to get a rolling summary,
decisions, action items, and a follow-up draft. The model receives the transcript
excerpt and your notes subject to the profile's PII policy. **Your notes** stay
as you wrote them. The notes model treats them as authoritative when they
disagree with the transcript.

After **Stop**, wait for the last transcript chunk, then choose **Save meeting**.
This commits a Meeting, its linked transcript Document, and Tasks. The follow-up
is saved as a local draft and can be copied. A bound app can also use the
completion to start a workflow. Mail write-back requires an app with a connected
draft action and approval.

## Meeting capture for apps and workflows

`meetings.starting` checks bound local Calendar events for a starting conference.
`meetings.notes` returns typed notes from the configured model.
`meetings.transcribe` handles optional cloud fallback under PII policy.
`meetings.ingest` saves one final capture per recording ID. Apps with the common
`event` binding can use a `record.changed` trigger filtered by
`changed('capture_status') && capture_status == 'ended'`. The transcript
Document and task links are present when the event is consumed. Transcript,
notes, and draft text are PII fields and follow the data store's masking policy.
