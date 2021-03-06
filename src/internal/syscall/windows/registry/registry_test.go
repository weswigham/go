// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build windows

package registry_test

import (
	"bytes"
	"crypto/rand"
	"os"
	"syscall"
	"testing"

	"internal/syscall/windows/registry"
)

func randKeyName(prefix string) string {
	const numbers = "0123456789"
	buf := make([]byte, 10)
	rand.Read(buf)
	for i, b := range buf {
		buf[i] = numbers[b%byte(len(numbers))]
	}
	return prefix + string(buf)
}

func TestReadSubKeyNames(t *testing.T) {
	k, err := registry.OpenKey(registry.CLASSES_ROOT, "TypeLib", registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()

	names, err := k.ReadSubKeyNames(-1)
	if err != nil {
		t.Fatal(err)
	}
	var foundStdOle bool
	for _, name := range names {
		// Every PC has "stdole 2.0 OLE Automation" library installed.
		if name == "{00020430-0000-0000-C000-000000000046}" {
			foundStdOle = true
		}
	}
	if !foundStdOle {
		t.Fatal("could not find stdole 2.0 OLE Automation")
	}
}

func TestCreateOpenDeleteKey(t *testing.T) {
	k, err := registry.OpenKey(registry.CURRENT_USER, "Software", registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()

	testKName := randKeyName("TestCreateOpenDeleteKey_")

	testK, exist, err := registry.CreateKey(k, testKName, registry.CREATE_SUB_KEY)
	if err != nil {
		t.Fatal(err)
	}
	defer testK.Close()

	if exist {
		t.Fatalf("key %q already exists", testKName)
	}

	testKAgain, exist, err := registry.CreateKey(k, testKName, registry.CREATE_SUB_KEY)
	if err != nil {
		t.Fatal(err)
	}
	defer testKAgain.Close()

	if !exist {
		t.Fatalf("key %q should already exist", testKName)
	}

	testKOpened, err := registry.OpenKey(k, testKName, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		t.Fatal(err)
	}
	defer testKOpened.Close()

	err = registry.DeleteKey(k, testKName)
	if err != nil {
		t.Fatal(err)
	}

	testKOpenedAgain, err := registry.OpenKey(k, testKName, registry.ENUMERATE_SUB_KEYS)
	if err == nil {
		defer testKOpenedAgain.Close()
		t.Fatalf("key %q should already been deleted", testKName)
	}
	if err != registry.ErrNotExist {
		t.Fatalf(`unexpected error ("not exist" expected): %v`, err)
	}
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	if a == nil {
		return true
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type ValueTest struct {
	Type     uint32
	Name     string
	Value    interface{}
	WillFail bool
}

var ValueTests = []ValueTest{
	{Type: registry.SZ, Name: "String1", Value: ""},
	{Type: registry.SZ, Name: "String2", Value: "\000", WillFail: true},
	{Type: registry.SZ, Name: "String3", Value: "Hello World"},
	{Type: registry.SZ, Name: "String4", Value: "Hello World\000", WillFail: true},
	{Type: registry.EXPAND_SZ, Name: "ExpString1", Value: ""},
	{Type: registry.EXPAND_SZ, Name: "ExpString2", Value: "\000", WillFail: true},
	{Type: registry.EXPAND_SZ, Name: "ExpString3", Value: "Hello World"},
	{Type: registry.EXPAND_SZ, Name: "ExpString4", Value: "Hello\000World", WillFail: true},
	{Type: registry.EXPAND_SZ, Name: "ExpString5", Value: "%PATH%"},
	{Type: registry.EXPAND_SZ, Name: "ExpString6", Value: "%NO_SUCH_VARIABLE%"},
	{Type: registry.EXPAND_SZ, Name: "ExpString7", Value: "%PATH%;."},
	{Type: registry.BINARY, Name: "Binary1", Value: []byte{}},
	{Type: registry.BINARY, Name: "Binary2", Value: []byte{1, 2, 3}},
	{Type: registry.BINARY, Name: "Binary3", Value: []byte{3, 2, 1, 0, 1, 2, 3}},
	{Type: registry.DWORD, Name: "Dword1", Value: uint64(0)},
	{Type: registry.DWORD, Name: "Dword2", Value: uint64(1)},
	{Type: registry.DWORD, Name: "Dword3", Value: uint64(0xff)},
	{Type: registry.DWORD, Name: "Dword4", Value: uint64(0xffff)},
	{Type: registry.QWORD, Name: "Qword1", Value: uint64(0)},
	{Type: registry.QWORD, Name: "Qword2", Value: uint64(1)},
	{Type: registry.QWORD, Name: "Qword3", Value: uint64(0xff)},
	{Type: registry.QWORD, Name: "Qword4", Value: uint64(0xffff)},
	{Type: registry.QWORD, Name: "Qword5", Value: uint64(0xffffff)},
	{Type: registry.QWORD, Name: "Qword6", Value: uint64(0xffffffff)},
	{Type: registry.MULTI_SZ, Name: "MultiString1", Value: []string{"a", "b", "c"}},
	{Type: registry.MULTI_SZ, Name: "MultiString2", Value: []string{"abc", "", "cba"}},
	{Type: registry.MULTI_SZ, Name: "MultiString3", Value: []string{""}},
	{Type: registry.MULTI_SZ, Name: "MultiString4", Value: []string{"abcdef"}},
	{Type: registry.MULTI_SZ, Name: "MultiString5", Value: []string{"\000"}, WillFail: true},
	{Type: registry.MULTI_SZ, Name: "MultiString6", Value: []string{"a\000b"}, WillFail: true},
	{Type: registry.MULTI_SZ, Name: "MultiString7", Value: []string{"ab", "\000", "cd"}, WillFail: true},
	{Type: registry.MULTI_SZ, Name: "MultiString8", Value: []string{"\000", "cd"}, WillFail: true},
	{Type: registry.MULTI_SZ, Name: "MultiString9", Value: []string{"ab", "\000"}, WillFail: true},
}

func setValues(t *testing.T, k registry.Key) {
	for _, test := range ValueTests {
		var err error
		switch test.Type {
		case registry.SZ:
			err = k.SetStringValue(test.Name, test.Value.(string))
		case registry.EXPAND_SZ:
			err = k.SetExpandStringValue(test.Name, test.Value.(string))
		case registry.MULTI_SZ:
			err = k.SetStringsValue(test.Name, test.Value.([]string))
		case registry.BINARY:
			err = k.SetBinaryValue(test.Name, test.Value.([]byte))
		case registry.DWORD:
			err = k.SetDWordValue(test.Name, uint32(test.Value.(uint64)))
		case registry.QWORD:
			err = k.SetQWordValue(test.Name, test.Value.(uint64))
		default:
			t.Fatalf("unsupported type %d for %s value", test.Type, test.Name)
		}
		if test.WillFail {
			if err == nil {
				t.Fatalf("setting %s value %q should fail, but succeeded", test.Name, test.Value)
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

func enumerateValues(t *testing.T, k registry.Key) {
	names, err := k.ReadValueNames(-1)
	if err != nil {
		t.Error(err)
		return
	}
	haveNames := make(map[string]bool)
	for _, n := range names {
		haveNames[n] = false
	}
	for _, test := range ValueTests {
		wantFound := !test.WillFail
		_, haveFound := haveNames[test.Name]
		if wantFound && !haveFound {
			t.Errorf("value %s is not found while enumerating", test.Name)
		}
		if haveFound && !wantFound {
			t.Errorf("value %s is found while enumerating, but expected to fail", test.Name)
		}
		if haveFound {
			delete(haveNames, test.Name)
		}
	}
	for n, v := range haveNames {
		t.Errorf("value %s (%v) is found while enumerating, but has not been cretaed", n, v)
	}
}

func testErrNotExist(t *testing.T, name string, err error) {
	if err == nil {
		t.Errorf("%s value should not exist", name)
		return
	}
	if err != registry.ErrNotExist {
		t.Errorf("reading %s value should return 'not exist' error, but got: %s", name, err)
		return
	}
}

func testErrUnexpectedType(t *testing.T, test ValueTest, gottype uint32, err error) {
	if err == nil {
		t.Errorf("GetXValue(%q) should not succeed", test.Name)
		return
	}
	if err != registry.ErrUnexpectedType {
		t.Errorf("reading %s value should return 'unexpected key value type' error, but got: %s", test.Name, err)
		return
	}
	if gottype != test.Type {
		t.Errorf("want %s value type %v, got %v", test.Name, test.Type, gottype)
		return
	}
}

func testGetStringValue(t *testing.T, k registry.Key, test ValueTest) {
	got, gottype, err := k.GetStringValue(test.Name)
	if err != nil {
		t.Errorf("GetStringValue(%s) failed: %v", test.Name, err)
		return
	}
	if got != test.Value {
		t.Errorf("want %s value %q, got %q", test.Name, test.Value, got)
		return
	}
	if gottype != test.Type {
		t.Errorf("want %s value type %v, got %v", test.Name, test.Type, gottype)
		return
	}
	if gottype == registry.EXPAND_SZ {
		_, err = registry.ExpandString(got)
		if err != nil {
			t.Errorf("ExpandString(%s) failed: %v", got, err)
			return
		}
	}
}

func testGetIntegerValue(t *testing.T, k registry.Key, test ValueTest) {
	got, gottype, err := k.GetIntegerValue(test.Name)
	if err != nil {
		t.Errorf("GetIntegerValue(%s) failed: %v", test.Name, err)
		return
	}
	if got != test.Value.(uint64) {
		t.Errorf("want %s value %v, got %v", test.Name, test.Value, got)
		return
	}
	if gottype != test.Type {
		t.Errorf("want %s value type %v, got %v", test.Name, test.Type, gottype)
		return
	}
}

func testGetBinaryValue(t *testing.T, k registry.Key, test ValueTest) {
	got, gottype, err := k.GetBinaryValue(test.Name)
	if err != nil {
		t.Errorf("GetBinaryValue(%s) failed: %v", test.Name, err)
		return
	}
	if !bytes.Equal(got, test.Value.([]byte)) {
		t.Errorf("want %s value %v, got %v", test.Name, test.Value, got)
		return
	}
	if gottype != test.Type {
		t.Errorf("want %s value type %v, got %v", test.Name, test.Type, gottype)
		return
	}
}

func testGetStringsValue(t *testing.T, k registry.Key, test ValueTest) {
	got, gottype, err := k.GetStringsValue(test.Name)
	if err != nil {
		t.Errorf("GetStringsValue(%s) failed: %v", test.Name, err)
		return
	}
	if !equalStringSlice(got, test.Value.([]string)) {
		t.Errorf("want %s value %#v, got %#v", test.Name, test.Value, got)
		return
	}
	if gottype != test.Type {
		t.Errorf("want %s value type %v, got %v", test.Name, test.Type, gottype)
		return
	}
}

func testGetValue(t *testing.T, k registry.Key, test ValueTest, size int) {
	if size <= 0 {
		return
	}
	// read data with no buffer
	gotsize, gottype, err := k.GetValue(test.Name, nil)
	if err != nil {
		t.Errorf("GetValue(%s, [%d]byte) failed: %v", test.Name, size, err)
		return
	}
	if gotsize != size {
		t.Errorf("want %s value size of %d, got %v", test.Name, size, gotsize)
		return
	}
	if gottype != test.Type {
		t.Errorf("want %s value type %v, got %v", test.Name, test.Type, gottype)
		return
	}
	// read data with short buffer
	gotsize, gottype, err = k.GetValue(test.Name, make([]byte, size-1))
	if err == nil {
		t.Errorf("GetValue(%s, [%d]byte) should fail, but suceeded", test.Name, size-1)
		return
	}
	if err != registry.ErrShortBuffer {
		t.Errorf("reading %s value should return 'short buffer' error, but got: %s", test.Name, err)
		return
	}
	if gotsize != size {
		t.Errorf("want %s value size of %d, got %v", test.Name, size, gotsize)
		return
	}
	if gottype != test.Type {
		t.Errorf("want %s value type %v, got %v", test.Name, test.Type, gottype)
		return
	}
	// read full data
	gotsize, gottype, err = k.GetValue(test.Name, make([]byte, size))
	if err != nil {
		t.Errorf("GetValue(%s, [%d]byte) failed: %v", test.Name, size, err)
		return
	}
	if gotsize != size {
		t.Errorf("want %s value size of %d, got %v", test.Name, size, gotsize)
		return
	}
	if gottype != test.Type {
		t.Errorf("want %s value type %v, got %v", test.Name, test.Type, gottype)
		return
	}
	// check GetValue returns ErrNotExist as required
	_, _, err = k.GetValue(test.Name+"_not_there", make([]byte, size))
	if err == nil {
		t.Errorf("GetValue(%q) should not succeed", test.Name)
		return
	}
	if err != registry.ErrNotExist {
		t.Errorf("GetValue(%q) should return 'not exist' error, but got: %s", test.Name, err)
		return
	}
}

func testValues(t *testing.T, k registry.Key) {
	for _, test := range ValueTests {
		switch test.Type {
		case registry.SZ, registry.EXPAND_SZ:
			if test.WillFail {
				_, _, err := k.GetStringValue(test.Name)
				testErrNotExist(t, test.Name, err)
			} else {
				testGetStringValue(t, k, test)
				_, gottype, err := k.GetIntegerValue(test.Name)
				testErrUnexpectedType(t, test, gottype, err)
				// Size of utf16 string in bytes is not perfect,
				// but correct for current test values.
				// Size also includes terminating 0.
				testGetValue(t, k, test, (len(test.Value.(string))+1)*2)
			}
			_, _, err := k.GetStringValue(test.Name + "_string_not_created")
			testErrNotExist(t, test.Name+"_string_not_created", err)
		case registry.DWORD, registry.QWORD:
			testGetIntegerValue(t, k, test)
			_, gottype, err := k.GetBinaryValue(test.Name)
			testErrUnexpectedType(t, test, gottype, err)
			_, _, err = k.GetIntegerValue(test.Name + "_int_not_created")
			testErrNotExist(t, test.Name+"_int_not_created", err)
			size := 8
			if test.Type == registry.DWORD {
				size = 4
			}
			testGetValue(t, k, test, size)
		case registry.BINARY:
			testGetBinaryValue(t, k, test)
			_, gottype, err := k.GetStringsValue(test.Name)
			testErrUnexpectedType(t, test, gottype, err)
			_, _, err = k.GetBinaryValue(test.Name + "_byte_not_created")
			testErrNotExist(t, test.Name+"_byte_not_created", err)
			testGetValue(t, k, test, len(test.Value.([]byte)))
		case registry.MULTI_SZ:
			if test.WillFail {
				_, _, err := k.GetStringsValue(test.Name)
				testErrNotExist(t, test.Name, err)
			} else {
				testGetStringsValue(t, k, test)
				_, gottype, err := k.GetStringValue(test.Name)
				testErrUnexpectedType(t, test, gottype, err)
				size := 0
				for _, s := range test.Value.([]string) {
					size += len(s) + 1 // nil terminated
				}
				size += 1 // extra nil at the end
				size *= 2 // count bytes, not uint16
				testGetValue(t, k, test, size)
			}
			_, _, err := k.GetStringsValue(test.Name + "_strings_not_created")
			testErrNotExist(t, test.Name+"_strings_not_created", err)
		default:
			t.Errorf("unsupported type %d for %s value", test.Type, test.Name)
			continue
		}
	}
}

func testStat(t *testing.T, k registry.Key) {
	subk, _, err := registry.CreateKey(k, "subkey", registry.CREATE_SUB_KEY)
	if err != nil {
		t.Error(err)
		return
	}
	defer subk.Close()

	defer registry.DeleteKey(k, "subkey")

	ki, err := k.Stat()
	if err != nil {
		t.Error(err)
		return
	}
	if ki.SubKeyCount != 1 {
		t.Error("key must have 1 subkey")
	}
	if ki.MaxSubKeyLen != 6 {
		t.Error("key max subkey name length must be 6")
	}
	if ki.ValueCount != 24 {
		t.Errorf("key must have 24 values, but is %d", ki.ValueCount)
	}
	if ki.MaxValueNameLen != 12 {
		t.Errorf("key max value name length must be 10, but is %d", ki.MaxValueNameLen)
	}
	if ki.MaxValueLen != 38 {
		t.Errorf("key max value length must be 38, but is %d", ki.MaxValueLen)
	}
}

func deleteValues(t *testing.T, k registry.Key) {
	for _, test := range ValueTests {
		if test.WillFail {
			continue
		}
		err := k.DeleteValue(test.Name)
		if err != nil {
			t.Error(err)
			continue
		}
	}
	names, err := k.ReadValueNames(-1)
	if err != nil {
		t.Error(err)
		return
	}
	if len(names) != 0 {
		t.Errorf("some values remain after deletion: %v", names)
	}
}

func TestValues(t *testing.T) {
	softwareK, err := registry.OpenKey(registry.CURRENT_USER, "Software", registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer softwareK.Close()

	testKName := randKeyName("TestValues_")

	k, exist, err := registry.CreateKey(softwareK, testKName, registry.CREATE_SUB_KEY|registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()

	if exist {
		t.Fatalf("key %q already exists", testKName)
	}

	defer registry.DeleteKey(softwareK, testKName)

	setValues(t, k)

	enumerateValues(t, k)

	testValues(t, k)

	testStat(t, k)

	deleteValues(t, k)
}

func walkKey(t *testing.T, k registry.Key, kname string) {
	names, err := k.ReadValueNames(-1)
	if err != nil {
		t.Fatalf("reading value names of %s failed: %v", kname, err)
	}
	for _, name := range names {
		_, valtype, err := k.GetValue(name, nil)
		if err != nil {
			t.Fatalf("reading value type of %s of %s failed: %v", name, kname, err)
		}
		switch valtype {
		case registry.NONE:
		case registry.SZ:
			_, _, err := k.GetStringValue(name)
			if err != nil {
				t.Error(err)
			}
		case registry.EXPAND_SZ:
			s, _, err := k.GetStringValue(name)
			if err != nil {
				t.Error(err)
			}
			_, err = registry.ExpandString(s)
			if err != nil {
				t.Error(err)
			}
		case registry.DWORD, registry.QWORD:
			_, _, err := k.GetIntegerValue(name)
			if err != nil {
				t.Error(err)
			}
		case registry.BINARY:
			_, _, err := k.GetBinaryValue(name)
			if err != nil {
				t.Error(err)
			}
		case registry.MULTI_SZ:
			_, _, err := k.GetStringsValue(name)
			if err != nil {
				t.Error(err)
			}
		case registry.FULL_RESOURCE_DESCRIPTOR, registry.RESOURCE_LIST, registry.RESOURCE_REQUIREMENTS_LIST:
			// TODO: not implemented
		default:
			t.Fatalf("value type %d of %s of %s failed: %v", valtype, name, kname, err)
		}
	}

	names, err = k.ReadSubKeyNames(-1)
	if err != nil {
		t.Fatalf("reading sub-keys of %s failed: %v", kname, err)
	}
	for _, name := range names {
		func() {
			subk, err := registry.OpenKey(k, name, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
			if err != nil {
				if err == syscall.ERROR_ACCESS_DENIED {
					// ignore error, if we are not allowed to access this key
					return
				}
				t.Fatalf("opening sub-keys %s of %s failed: %v", name, kname, err)
			}
			defer subk.Close()

			walkKey(t, subk, kname+`\`+name)
		}()
	}
}

func TestWalkFullRegistry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long running test in short mode")
	}
	walkKey(t, registry.CLASSES_ROOT, "CLASSES_ROOT")
	walkKey(t, registry.CURRENT_USER, "CURRENT_USER")
	walkKey(t, registry.LOCAL_MACHINE, "LOCAL_MACHINE")
	walkKey(t, registry.USERS, "USERS")
	walkKey(t, registry.CURRENT_CONFIG, "CURRENT_CONFIG")
}

func TestExpandString(t *testing.T) {
	got, err := registry.ExpandString("%PATH%")
	if err != nil {
		t.Fatal(err)
	}
	want := os.Getenv("PATH")
	if got != want {
		t.Errorf("want %q string expanded, got %q", want, got)
	}
}
