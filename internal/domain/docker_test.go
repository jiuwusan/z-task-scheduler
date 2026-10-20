package domain

import "testing"

func TestDockerTaskRequiresContainerName(t *testing.T) {
	for _, name := range []string{"media", "media-server_1", " /media "} {
		task := Task{Name: "docker", Type: "docker", ContainerName: name, DockerAction: "restart"}
		if err := ValidateTask(&task); err != nil || task.ContainerName == " /media " {
			t.Fatal(task, err)
		}
	}
	for _, name := range []string{"", "media/other", "../media", "media?x=1", "media server"} {
		task := Task{Name: "docker", Type: "docker", ContainerName: name, ContainerID: "old-id", DockerAction: "restart"}
		if err := ValidateTask(&task); err == nil {
			t.Fatal("invalid container name accepted", name)
		}
	}
}
