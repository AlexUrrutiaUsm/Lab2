package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	pb "gim/grpc-server/proto"
	"log"
	"math"
	"math/rand"
	"net"
	"os"
	"strings"
	"time"

	"github.com/streadway/amqp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func EncriptarAES256(datos []byte, clave string) (string, error) {
	key := []byte(clave)
	if len(key) < 32 {
		newKey := make([]byte, 32)
		copy(newKey, key)
		key = newKey
	} else if len(key) > 32 {
		key = key[:32]
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, aesGCM.NonceSize())
	_, err = rand.Read(nonce)
	if err != nil {
		return "", err
	}

	ciphertext := aesGCM.Seal(nonce, nonce, datos, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func Mandar(queueInfo *pb.AsignacionCombateResponse, clave string) {
	var errFinal error
	for intento := 1; intento <= 3; intento++ {
		conn, err := amqp.Dial("amqp://guest:guest@host.docker.internal:5672/")
		if err != nil {
			errFinal = fmt.Errorf("intento %d: error al conectar: %v", intento, err)
			time.Sleep(1 * time.Second)
			continue
		}

		channel, err := conn.Channel()
		if err != nil {
			errFinal = fmt.Errorf("intento %d: error al abrir el canal: %v", intento, err)
			conn.Close()
			time.Sleep(1 * time.Second)
			continue
		}

		queue, err := channel.QueueDeclare(
			"CDP", false, false, false, false, nil,
		)
		if err != nil {
			errFinal = fmt.Errorf("intento %d: error al declarar la cola: %v", intento, err)
			channel.Close()
			conn.Close()
			time.Sleep(1 * time.Second)
			continue
		}

		body, err := json.Marshal(queueInfo)
		if err != nil {
			errFinal = fmt.Errorf("intento %d: error al serializar queueInfo: %v", intento, err)
			channel.Close()
			conn.Close()
			time.Sleep(1 * time.Second)
			continue
		}

		encriptado, err := EncriptarAES256(body, clave)
		if err != nil {
			errFinal = fmt.Errorf("intento %d: error al encriptar el mensaje: %v", intento, err)
			channel.Close()
			conn.Close()
			time.Sleep(1 * time.Second)
			continue
		}

		err = channel.Publish(
			"", queue.Name, false, false,
			amqp.Publishing{
				ContentType: "text/plain",
				Body:        []byte(encriptado),
			},
		)
		channel.Close()
		conn.Close()
		if err != nil {
			errFinal = fmt.Errorf("intento %d: error al publicar en la cola CDP: %v", intento, err)
			time.Sleep(1 * time.Second)
			continue
		}

		log.Printf("Enviado a CDP (encriptado): %s", encriptado[:20]+"...")
		return
	}

	f, ferr := os.OpenFile("log_errores.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if ferr == nil {
		f.WriteString(fmt.Sprintf("%s - Error al enviar resultado: %v\n", time.Now().Format(time.RFC3339), errFinal))
		f.Close()
	}
	log.Printf("No se pudo enviar el resultado tras 3 intentos. Error: %v", errFinal)
}

type GimServer struct {
	pb.UnimplementedGimServiceServer
	Gimnasios []Gimnasio
}

type Gimnasio struct {
	ID     int
	Region string
	Clave  string
}

func NewGimServer() *GimServer {
	rand.Seed(time.Now().UnixNano())
	regionesNombres := []string{"Kanto", "Johto", "Hoenn", "Sinnoh", "Unova", "Kalos", "Alola", "Galar", "Paldea"}
	var gimnasios []Gimnasio

	for i, nombre := range regionesNombres {
		envVar := "AES_KEY_" + strings.ToUpper(nombre)
		clave := os.Getenv(envVar)
		gimnasios = append(gimnasios, Gimnasio{
			ID:     i,
			Region: nombre,
			Clave:  clave,
		})
	}

	return &GimServer{
		Gimnasios: gimnasios,
	}
}

func SimularCombate(ent1, ent2 *pb.EntrenadorCombate) *pb.EntrenadorCombate {
	diff := float64(ent1.Ranking - ent2.Ranking)
	k := 100.0
	prob := 1.0 / (1.0 + math.Exp(-diff/k))
	if rand.Float64() <= prob {
		return ent1
	}
	return ent2
}

func (s *GimServer) AsignarCombate(ctx context.Context, req *pb.AsignacionCombateRequest) (*pb.AsignacionCombateResponse, error) {
	var gimnasio *Gimnasio
	for i := range s.Gimnasios {
		if strings.EqualFold(s.Gimnasios[i].Region, req.Region) {
			gimnasio = &s.Gimnasios[i]
			break
		}
	}
	if gimnasio == nil {
		return nil, fmt.Errorf("no se encontró gimnasio para la región %s", req.Region)
	}
	var ganador *pb.EntrenadorCombate
	if math.Abs(float64(req.Entrenador_1.Ranking-req.Entrenador_2.Ranking)) > 300 {
		if float64(req.Entrenador_1.Ranking-req.Entrenador_2.Ranking) > 0 {
			ganador = req.Entrenador_1
		} else {
			ganador = req.Entrenador_2
		}
	} else {
		ganador = SimularCombate(req.Entrenador_1, req.Entrenador_2)
	}

	fecha := time.Now().Format("2006-01-02")

	ACR := &pb.AsignacionCombateResponse{
		TorneoId:           req.TorneoId,
		IdEntrenador_1:     req.Entrenador_1.Id,
		NombreEntrenador_1: req.Entrenador_1.Nombre,
		IdEntrenador_2:     req.Entrenador_2.Id,
		NombreEntrenador_2: req.Entrenador_2.Nombre,
		IdGanador:          ganador.Id,
		NombreGanador:      ganador.Nombre,
		Fecha:              fecha,
		TipoMensaje:        "resultado_combate",
	}

	go Mandar(ACR, gimnasio.Clave)

	return &pb.AsignacionCombateResponse{}, nil
}

func main() {
	listener, err := net.Listen("tcp", ":50053")
	if err != nil {
		log.Fatalf("Error al iniciar el servidor: %v", err)
	}

	server := grpc.NewServer()
	pb.RegisterGimServiceServer(server, NewGimServer())
	reflection.Register(server)

	log.Println("Servidor de los gimnasios Pokemon en el puerto 50053")
	if err := server.Serve(listener); err != nil {
		log.Fatalf("Error al iniciar el servidor: %v", err)
	}
}
