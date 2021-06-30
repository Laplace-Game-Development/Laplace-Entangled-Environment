package startup

import "testing"

func TestInitServerStartupOnTaskList(t *testing.T) {
	channel := make(chan bool, 3)
	sender := func() (func(), error) {
		channel <- true
		return func() {
			channel <- false
		}, nil
	}

	taskList := []ServerTask{
		sender,
		sender,
		sender,
	}

	cleanUps := InitServerStartupOnTaskList(taskList)
	for i := 0; i < 3; i++ {
		select {
		case derp := <-channel:
			if !derp {
				t.Fatalf("Received False On Startup Rather than True!\n")
			}
		default:
			t.Fatalf("Channel Was Incomplete! Less than 3 sends!\n")
		}
	}

	cleanUps()
	for i := 0; i < 3; i++ {
		select {
		case derp := <-channel:
			if derp {
				t.Fatalf("Received True On Cleanup Rather than False!\n")
			}
		default:
			t.Fatalf("Channel Was Incomplete! Less than 3 sends!\n")
		}
	}
}
