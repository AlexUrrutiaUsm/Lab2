package main

import (
	"log"

	"github.com/streadway/amqp"
)

func main() {
	conn, err := amqp.Dial("amqp://guest:guest@10.35.168.24:5672/")
	if err != nil {
		log.Fatalf(" Error al conectar : %v", err)
	}
	defer conn.Close()

	channel, err := conn.Channel()
	if err != nil {
		log.Fatalf(" Error al abrir el canal: %v", err)
	}
	defer channel.Close()

	ownQueue, err := channel.QueueDeclare(
		"s",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf(" Error al declarar la cola propia: %v", err)
	}

	_, err = channel.QueueDeclare(
		"Entrenador",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf(" Error al declarar la cola Entrenador: %v", err)
	}

	msgs, err := channel.Consume(
		ownQueue.Name,
		"",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf(" Error al consumir la cola propia: %v", err)
	}

	log.Println("Escuchando la cola:", ownQueue.Name)
	forever := make(chan bool)
	go func() {
		for msg := range msgs {
			log.Printf("Procesando: %s", msg.Body)
			var err error
			maxRetries := 3
			for i := 1; i <= maxRetries; i++ {
				err = channel.Publish(
					"",
					"Entrenador",
					false,
					false,
					amqp.Publishing{
						ContentType: "text/plain",
						Body:        msg.Body,
					},
				)
				if err == nil {
					log.Printf("Mensaje reenviado a Entrenador: %s", msg.Body)
					break
				}
				log.Printf("Error al publicar en Entrenador (intento %d/%d): %v", i, maxRetries, err)
			}
			if err != nil {
				log.Printf("No se pudo reenviar el mensaje después de %d intentos: %s", maxRetries, msg.Body)
			}
		}
	}()
	<-forever
}
