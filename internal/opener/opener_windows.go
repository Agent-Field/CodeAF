package opener

func platformCommand() (string, []string) {
	return "rundll32.exe", []string{"url.dll,FileProtocolHandler"}
}
