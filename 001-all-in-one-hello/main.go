package main

func main() {
	go devservermain()
	go workermain()
	go clientmain()
	select {}
}
