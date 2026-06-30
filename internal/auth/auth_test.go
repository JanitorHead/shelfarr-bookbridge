package auth

import "testing"

func TestHashVerify(t *testing.T) {
	h, err := Hash("hunter2")
	if err != nil {
		t.Fatal(err)
	}
	if !Verify(h, "hunter2") {
		t.Fatal("correct password should verify")
	}
	if Verify(h, "wrong") {
		t.Fatal("wrong password must not verify")
	}
	if Verify("garbage", "hunter2") {
		t.Fatal("malformed hash must not verify")
	}
}

func TestIsLocalAddr(t *testing.T) {
	for _, a := range []string{"127.0.0.1:54321", "192.168.1.5:80", "10.0.0.2:1", "[::1]:7373", "172.16.3.4:9"} {
		if !IsLocalAddr(a) {
			t.Errorf("%s should be local", a)
		}
	}
	for _, a := range []string{"8.8.8.8:443", "203.0.113.7:80"} {
		if IsLocalAddr(a) {
			t.Errorf("%s should NOT be local", a)
		}
	}
}
