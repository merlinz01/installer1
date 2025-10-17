package main

import (
	"log"

	"github.com/merlinz01/installer1"
)

func Install(i *installer1.Installer) error {
	log.Println("Installing...")
	i.SetInDir(".")
	i.SetOutDir("C:/installer1_test")
	i.Dir("testdir", "testdir")
	i.File("test.txt", "testfile.txt")
	return nil
}

func Uninstall(i *installer1.Installer) error {
	log.Println("Uninstalling...")
	return nil
}
