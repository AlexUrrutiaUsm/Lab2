package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"sync"
	"time"

	pb "entrenador-cdp/grpc-server/proto"

	"github.com/streadway/amqp"
	"google.golang.org/grpc"
)

type AsignacionCombateResponse struct {
	TorneoId           string `json:"TorneoId"`
	IdEntrenador_1     string `json:"IdEntrenador_1"`
	NombreEntrenador_1 string `json:"NombreEntrenador_1"`
	IdEntrenador_2     string `json:"IdEntrenador_2"`
	NombreEntrenador_2 string `json:"NombreEntrenador_2"`
	IdGanador          string `json:"IdGanador"`
	NombreGanador      string `json:"NombreGanador"`
	Fecha              string `json:"Fecha"`
	TipoMensaje        string `json:"TipoMensaje"`
}

var (
	notificaciones     []string
	notiMutex          sync.Mutex
	historialFile      = "historial_entrenador.json"
	mensajesProcesados = make(map[string]bool)
)

func DesencriptarAES256(encriptado string, clave string) ([]byte, error) {
	key := []byte(clave)
	if len(key) < 32 {
		key = append(key, make([]byte, 32-len(key))...)
	} else if len(key) > 32 {
		key = key[:32]
	}
	ciphertext, err := base64.StdEncoding.DecodeString(encriptado)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := aesGCM.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, err
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}

func validarEntrenadoresConLCP(lcpClient pb.LcpServiceClient, id1, id2 string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resp1, err1 := lcpClient.ConsultarEntrenador(ctx, &pb.ConsultarEntrenadorRequest{EntrenadorId: id1})
	resp2, err2 := lcpClient.ConsultarEntrenador(ctx, &pb.ConsultarEntrenadorRequest{EntrenadorId: id2})
	if err1 != nil || err2 != nil {
		return false
	}
	return resp1.Estado == "Activo" && resp2.Estado == "Activo"
}

func guardarHistorialLocal(id string, evento string) {
	historial := map[string][]string{}
	data, err := ioutil.ReadFile(historialFile)
	if err == nil {
		_ = json.Unmarshal(data, &historial)
	}
	historial[id] = append(historial[id], evento)
	data, _ = json.MarshalIndent(historial, "", "  ")
	_ = ioutil.WriteFile(historialFile, data, 0644)
}

func consumirColaNoti() {
	conn, err := amqp.Dial("amqp://guest:guest@host.docker.internal:5672/")
	if err != nil {
		return
	}
	defer conn.Close()

	channel, err := conn.Channel()
	if err != nil {
		return
	}
	defer channel.Close()

	queue, err := channel.QueueDeclare(
		"Entrenador", false, false, false, false, nil,
	)
	if err != nil {
		return
	}

	msgs, err := channel.Consume(
		queue.Name, "", true, false, false, false, nil,
	)
	if err != nil {
		return
	}

	for msg := range msgs {
		notiMutex.Lock()
		notificaciones = append(notificaciones, string(msg.Body))
		notiMutex.Unlock()
		guardarHistorialLocal("notificaciones", string(msg.Body))
	}
}

func mostrarEstadoActual(lcpClient pb.LcpServiceClient, id string) {
	resp, err := lcpClient.ConsultarEntrenador(context.Background(), &pb.ConsultarEntrenadorRequest{EntrenadorId: id})
	if err != nil {
		fmt.Println("Error al consultar el estado del entrenador")
		return
	}
	fmt.Printf("ID: %s\nNombre: %s\nRegión: %s\nRanking: %d\nEstado: %s\nSuspensión: %d\n",
		resp.Id, resp.Nombre, resp.Region, resp.Ranking, resp.Estado, resp.Suspension)
}

func mostrarHistorialLocal(id string) {
	data, err := ioutil.ReadFile(historialFile)
	if err != nil {
		fmt.Println("No hay historial local.")
		return
	}
	historial := map[string][]string{}
	_ = json.Unmarshal(data, &historial)
	fmt.Println("Historial local:")
	for _, evento := range historial[id] {
		fmt.Println(" -", evento)
	}
}

func procesarColaCDP(lcpClient pb.LcpServiceClient) {
	conn, err := amqp.Dial("amqp://guest:guest@host.docker.internal:5672/")
	if err != nil {
		log.Fatalf("Error al conectar a RabbitMQ: %v", err)
	}
	defer conn.Close()

	channel, err := conn.Channel()
	if err != nil {
		log.Fatalf("Error al abrir el canal: %v", err)
	}
	defer channel.Close()

	queue, err := channel.QueueDeclare(
		"CDP", false, false, false, false, nil,
	)
	if err != nil {
		log.Fatalf("Error al declarar la cola CDP: %v", err)
	}

	msgs, err := channel.Consume(
		queue.Name, "", true, false, false, false, nil,
	)
	if err != nil {
		log.Fatalf("Error al consumir la cola CDP: %v", err)
	}

	log.Println("Escuchando la cola CDP...")
	clave := os.Getenv("AES_KEY_KANTO")
	for msg := range msgs {
		body, err := DesencriptarAES256(string(msg.Body), clave)
		if err != nil {
			log.Printf("Error al desencriptar mensaje: %v", err)
			continue
		}
		var acr AsignacionCombateResponse
		err = json.Unmarshal(body, &acr)
		if err != nil {
			log.Printf("Error al deserializar mensaje: %v", err)
			continue
		}
		claveUnica := acr.TorneoId + "_" + acr.Fecha
		if mensajesProcesados[claveUnica] {
			log.Printf("Mensaje duplicado: %s", claveUnica)
			continue
		}
		if validarEntrenadoresConLCP(lcpClient, acr.IdEntrenador_1, acr.IdEntrenador_2) {
			mensajesProcesados[claveUnica] = true
			bodyValido, _ := json.Marshal(acr)
			err = channel.Publish(
				"", "LCP", false, false,
				amqp.Publishing{
					ContentType: "application/json",
					Body:        bodyValido,
				},
			)
			if err != nil {
				log.Printf("Error al reenviar a LCP: %v", err)
			} else {
				log.Printf("Mensaje reenviado a LCP: %s", claveUnica)
			}
		} else {
			bodyRechazo, _ := json.Marshal(map[string]string{
				"motivo":  "entrenadores no válidos",
				"torneo":  acr.TorneoId,
				"fecha":   acr.Fecha,
				"mensaje": "rechazado",
			})
			err = channel.Publish(
				"", "SNP", false, false,
				amqp.Publishing{
					ContentType: "application/json",
					Body:        bodyRechazo,
				},
			)
			if err != nil {
				log.Printf("Error al reportar rechazo a SNP: %v", err)
			} else {
				log.Printf("Mensaje de rechazo enviado a SNP: %s", claveUnica)
			}
		}
	}
}

func main() {
	connLcp, err := grpc.Dial("host.docker.internal:50052", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("Error al conectar al servidor de la LCP: %v", err)
	}
	defer connLcp.Close()
	lcpClient := pb.NewLcpServiceClient(connLcp)

	go consumirColaNoti()
	go procesarColaCDP(lcpClient)

	for {
		fmt.Println("\nMenú:")
		fmt.Println("1. Consultar torneos disponibles")
		fmt.Println("2. Inscribirse en un torneo")
		fmt.Println("3. Ver notificaciones recibidas")
		fmt.Println("4. Ver estado actual")
		fmt.Println("5. Ver historial local")
		fmt.Println("6. Salir")
		fmt.Print("Seleccione una opción: ")

		var opcion int
		_, err := fmt.Scanln(&opcion)
		if err != nil {
			fmt.Println("Opción inválida. Intente de nuevo.")
			continue
		}

		switch opcion {
		case 1:
			resp, err := lcpClient.ConsultarTorneosDisponibles(context.Background(), &pb.TorneosDisponiblesRequest{})
			if err != nil {
				fmt.Println("Error al consultar los torneos disponibles")
				continue
			}
			fmt.Println("Los torneos disponibles son:")
			for _, torneo := range resp.Torneos {
				fmt.Printf("  Región: %s | ID Torneo: %s\n", torneo.Id, torneo.Region)
			}
		case 2:
			var id string
			fmt.Print("Ingrese su ID: ")
			fmt.Scanln(&id)
			var torneoid string
			fmt.Print("Ingrese el ID del torneo: ")
			fmt.Scanln(&torneoid)
			resp, err := lcpClient.InscribirseEnTorneo(context.Background(), &pb.InscripcionRequest{TorneoId: torneoid, EntrenadorId: id})
			if err != nil {
				fmt.Println("Error al inscribir en el torneo")
				continue
			}
			fmt.Println(resp.Inscripcion)
			guardarHistorialLocal(id, "Inscripción en torneo "+torneoid+": "+resp.Inscripcion)
		case 3:
			notiMutex.Lock()
			if len(notificaciones) == 0 {
				fmt.Println("No hay notificaciones nuevas.")
			} else {
				fmt.Println("Notificaciones recibidas:")
				for _, n := range notificaciones {
					fmt.Println(" -", n)
				}
			}
			notiMutex.Unlock()
		case 4:
			var id string
			fmt.Print("Ingrese su ID: ")
			fmt.Scanln(&id)
			mostrarEstadoActual(lcpClient, id)
		case 5:
			var id string
			fmt.Print("Ingrese su ID: ")
			fmt.Scanln(&id)
			mostrarHistorialLocal(id)
		case 6:
			fmt.Println("\n¡Hasta luego!")
			os.Exit(0)
		default:
			fmt.Println("Opción inválida. Intente de nuevo.")
		}
	}
}
