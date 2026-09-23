package contacts

// FormatPhone formats a ten-digit number as 555-123-4567. Anything that is
// not ten digits long is answered unchanged.
func FormatPhone(digits string) string {
	if len(digits) != 10 {
		return digits
	}
	return digits[:3] + "-" + digits[3:7] + "-" + digits[7:]
}
