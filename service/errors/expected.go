package errors

// IsExpected reports an explicitly classified application rejection. It only
// controls local logging; callers must still return the error and count failure.
// Do not infer this classification from error text or transport status codes.
func IsExpected(err error) bool {
	classified, ok := err.(interface{ ExpectedError() bool })
	return ok && classified.ExpectedError()
}
