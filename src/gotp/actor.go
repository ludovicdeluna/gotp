package gotp

import (
	"errors"
	"fmt"
	"time"
)

//public types

//the actor itself simply must define these functions
//DON'T IMPLEMENT THIS DIRECTLY, Implement GoActor
type Actor interface {
	//Passing in the pid here allows us to call self.StartChild, self.StartLink, etc
	Receive(msg Message) error

	Init(self Pid)

	StartChild(actor Actor) Pid
	StartLink(actor Actor) Pid

	Running() bool
	stop()
}

type GoActor struct {
	self Pid
	alive bool
}

func (ac *GoActor) Init(pid Pid) {
	ac.self = pid
	ac.alive = true
}

func (ac *GoActor) StartChild(actor Actor) Pid {
	childPid := Spawn(actor)
	watch := childPid.Watch()
	go func() {
		err := <- watch
		ac.self.recv <- Message{Payload:Stop{err: err, Sender: childPid}}
	}()
	return childPid
}

func (ac *GoActor) StartLink(actor Actor) Pid {
	linkPid := Spawn(actor)
	watch := linkPid.Watch()
	go func() {
		err := <- watch
		ac.self.stop <- err
	}()
	return linkPid
}

func (ac *GoActor) Running() bool {
	return ac.alive
}

func (ac *GoActor) stop() {
	ac.alive = false
}

type Message struct {
	Payload interface{}
}

type Stop struct {
	err error
	Sender Pid
}

type Unit struct{}

type Pid struct {
	//the channel over which to receive messages and deliver them to the actor
	recv chan Message
	//the channel to signal that the actor backing this pid should shut down
	stop chan error
	//the channel to signal a watcher that the actor backing this pid errored
	errored chan error
}

//send a message asynchronously to the pid
func (p *Pid) Send(msg interface{}) Unit {
	m := Message{msg}
	p.recv <- m
	return Unit{}
}

//begin the shutdown process of pid, and send on the returned channel when the shutdown finished
func (p *Pid) Stop() chan Unit {
	stopped := make(chan Unit)
	go func() {
		p.stop <- nil
		stopped <- Unit{}
	}()
	return stopped
}

//watch a pid for errors, and send on the returned channel if an error occured
func (p *Pid) Watch() chan error {
	errChan := make(chan error)
	go func() {
		err := <-p.errored
		errChan <- err
	}()
	return errChan
}

//create a new actor and return the pid, so you can send it messages
func Spawn(actor Actor) Pid {
	p := Pid{recv: make(chan Message), stop: make(chan error), errored: make(chan error)}
	actor.Init(p)
	//create the first wait barrier, and prime it for the first iteration of the receive loop
	//start the receive loop
	go recvLoop(p.recv, p, actor)
	return p
}

func makeError(i interface{}) error {
	return errors.New(fmt.Sprintf("%s", i))
}

func recvLoop(recv chan Message, p Pid, actor Actor) {
	//create the first nextwait channel
	nextWait := make(chan bool)
	firstWait := nextWait
	go func() {
		firstWait <- true
	}()
	//handle panics in this loop
	defer func() {
		if r := recover(); r != nil {
			p.errored <- makeError(r)
		}
	}()
	stop := make(chan error)
	//loop forever
	for {
		select {
		case err := <- stop:
			//do something with the error
			p.errored <- err
			actor.stop()
			return
		case <-p.stop:
			//do something with the stop
			actor.stop()
			return
		case received := <- p.recv:
			// fmt.Println("Received", received)
			currWait := nextWait
			nextWait = make(chan bool)
			opsNextWait := nextWait
			runFn := func() {
				// fmt.Println("runFn()", received)
				defer func() {
					if r := recover(); r != nil {
						stop <- makeError(r)
					}
				}()
				// fmt.Println("receiving on currWait", received, currWait, runtime.NumGoroutine())
				<-currWait
				// fmt.Println("received on currWait", received, currWait)
				err := actor.Receive(received)
				if err != nil {
					stop <- err
				}
				// fmt.Println("sending to opsNextWait", received, opsNextWait)
				opsNextWait <- true
				// fmt.Println("sent to opsNextWait", received, opsNextWait)
			}
			go runFn()
		case <-time.After(5*time.Second):
			fmt.Println("No messages in 5 seconds to", actor)
		}
	}
}
