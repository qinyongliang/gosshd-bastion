package server

import "testing"

func TestCleanSFTPPathWindowsAndPOSIX(t *testing.T) {
	for input, want := range map[string]string{
		`D:\3D\灯.scad`: "/D:/3D/灯.scad", "D:/3D/灯.scad": "/D:/3D/灯.scad", "d:/3D/灯.scad": "/D:/3D/灯.scad", "/D:/3D/灯.scad": "/D:/3D/灯.scad",
		"/home/user/work": "/home/user/work", `file\name`: `file\name`, `\\server\share\file`: "//server/share/file",
	} {
		if got := cleanSFTPPath(input); got != want {
			t.Errorf("cleanSFTPPath(%q) = %q, want %q", input, got, want)
		}
	}
	if !remotePathWithin(`D:\3D`, "/D:/3D/sub") {
		t.Fatal("mixed Windows path spellings must not bypass self-containing destination checks")
	}
}
