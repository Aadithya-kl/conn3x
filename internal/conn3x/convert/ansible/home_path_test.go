// Copyright (c) The Conn3x Authors
// SPDX-License-Identifier: MPL-2.0

package ansible

import (
	"strings"
	"testing"
)

// A path under ~ is the home of whoever the task runs as, which the
// converter can't know, so the task stays for review and the rest of the
// role still converts (#1445).
func TestPathUnderHomeStaysForReview(t *testing.T) {
	const why = "uses a path under ~, which depends on the user the task runs as; write the absolute path"
	for _, task := range []string{
		"file: {path: ~/.kube, state: directory}",
		"file: {name: ~/.kube, state: directory}",
		"file: {src: ~/app, dest: /usr/local/app, state: link}",
		"copy: {src: ~/x, dest: /x, remote_src: true}",
		"copy: {dest: ~/x, content: x}",
		"template: {src: a.j2, dest: ~/x}",
		"lineinfile: {path: ~/.bashrc, line: x}",
		"blockinfile: {dest: ~/.bashrc, block: x}",
		"command: {cmd: make, creates: ~/out}",
		"command: {cmd: make clean, removes: ~/out}",
		"command: {cmd: make, chdir: ~/src, creates: /opt/out}",
		"file: {path: '{{ home }}/.kube', state: directory}",
	} {
		dir := writeRole(t, map[string]string{
			"vars/main.yml":  "home: '~'\n",
			"templates/a.j2": "x\n",
			"tasks/main.yml": "- name: Ensure motd.\n  copy: {dest: /etc/motd, content: hi}\n\n- name: Ensure .kube directory exists.\n  " + task + "\n",
		})
		res, err := ConvertRole(dir)
		if err != nil {
			t.Fatal(err)
		}
		if res.Converted != 1 || len(res.Review) != 1 || res.Review[0].Reason != why {
			t.Errorf("%q: converted %d, review %+v", task, res.Converted, res.Review)
			continue
		}
		todo := `TODO(convert): Task "Ensure .kube directory exists." (tasks/main.yml line 4) ` + why + "."
		if hcl := string(res.Files["main.circuit.hcl"]); !strings.Contains(hcl, todo) {
			t.Errorf("%q: output lacks %q:\n%s", task, todo, hcl)
		}
	}
}

// Without remote_src, a copy or template src is a file on the controller,
// which the converter looks for beside the playbook or in the role, so it
// keeps that reason.
func TestControllerSrcUnderHomeKeepsItsReason(t *testing.T) {
	res := convertString(t, "- hosts: all\n  tasks:\n    - copy: {src: ~/files/a, dest: /etc/a}\n")
	if why := "~/files/a was not found beside the playbook or in files/"; len(res.Review) != 1 || res.Review[0].Reason != why {
		t.Errorf("playbook copy: review %+v", res.Review)
	}
	for task, why := range map[string]string{
		"copy: {src: ~/files/a, dest: /etc/a}":  "~/files/a was not found in the role's files/",
		"template: {src: ~/a.j2, dest: /etc/a}": "~/a.j2 was not found in the role's templates/",
	} {
		res, err := ConvertRole(writeRole(t, map[string]string{"tasks/main.yml": "- " + task + "\n"}))
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Review) != 1 || res.Review[0].Reason != why {
			t.Errorf("%q: review %+v", task, res.Review)
		}
	}
}

// ~ only starts a home path: elsewhere it's a character, and a bare ~ in
// YAML is null.
func TestPathWithTildeElsewhereConverts(t *testing.T) {
	for _, task := range []string{
		"file: {path: /opt/a~b, state: directory}",
		"copy: {dest: /etc/x~, content: x}",
	} {
		res := convertString(t, "- hosts: all\n  tasks:\n    - "+task+"\n")
		if res.Converted != 1 || len(res.Review) != 0 {
			t.Errorf("%q: converted %d, review %+v", task, res.Converted, res.Review)
		}
	}
	res := convertString(t, "- hosts: all\n  tasks:\n    - file: {path: ~, state: directory}\n")
	if strings.Contains(string(res.HCL), "path under ~") {
		t.Errorf("a null path was read as a path under ~:\n%s", res.HCL)
	}
}
