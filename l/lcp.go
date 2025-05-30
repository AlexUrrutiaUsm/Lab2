package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"strconv"
	"time"

	pb "lcp/grpc-server/proto"

	"github.com/streadway/amqp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type LcpServer struct {
	pb.UnimplementedLcpServiceServer
	Regiones             []Region
	Entrenadores         []Entrenador
	ResultadosProcesados map[string]bool
}

type Torneo struct {
	ID           string       `json:"id"`
	Estado       string       `json:"estado"`
	Region       string       `json:"region"`
	Combatientes []Entrenador `json:"combatientes"`
	Combates     []string     `json:"combates"`
}

type Region struct {
	Nombre  string   `json:"nombre"`
	Torneos []Torneo `json:"torneos"`
}

type Entrenador struct {
	ID         string `json:"id"`
	Nombre     string `json:"nombre"`
	Region     string `json:"region"`
	Ranking    int    `json:"ranking"`
	Estado     string `json:"estado"`
	Suspension int    `json:"suspension"`
}

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

func NotificarSNP(evento string, detalle string) {
	mensaje := map[string]string{
		"evento":  evento,
		"detalle": detalle,
		"fecha":   time.Now().Format(time.RFC3339),
	}
	body, _ := json.Marshal(mensaje)
	err := PublishToQueue("SNP", body)
	if err != nil {
		log.Printf("[SNP] Error al notificar: %v", err)
	} else {
		log.Printf("[SNP] Notificado: %s - %s", evento, detalle)
	}
}

func (s *LcpServer) ProcesarResultadoCombate(resultado AsignacionCombateResponse) error {
	if resultado.TorneoId == "" || resultado.IdEntrenador_1 == "" || resultado.IdEntrenador_2 == "" || resultado.IdGanador == "" {
		log.Printf("[AUDITORIA] Resultado corrupto: %+v", resultado)
		return fmt.Errorf("estructura de resultado inválida")
	}

	key := resultado.TorneoId + "-" + resultado.IdEntrenador_1 + "-" + resultado.IdEntrenador_2
	if s.ResultadosProcesados[key] {
		log.Printf("[AUDITORIA] Resultado duplicado: %+v", resultado)
		return fmt.Errorf("resultado duplicado")
	}

	var existe1, existe2, existeGanador bool
	for _, e := range s.Entrenadores {
		if e.ID == resultado.IdEntrenador_1 {
			existe1 = true
		}
		if e.ID == resultado.IdEntrenador_2 {
			existe2 = true
		}
		if e.ID == resultado.IdGanador {
			existeGanador = true
		}
	}
	if !existe1 || !existe2 || !existeGanador {
		log.Printf("[AUDITORIA] Resultado con entrenador no registrado: %+v", resultado)
		return fmt.Errorf("entrenador no registrado")
	}

	s.ResultadosProcesados[key] = true

	for j := range s.Entrenadores {
		if s.Entrenadores[j].ID == resultado.IdGanador {
			s.Entrenadores[j].Ranking += 10
			NotificarSNP("Ranking actualizado", fmt.Sprintf("Entrenador %s ganó y ahora tiene ranking %d", s.Entrenadores[j].Nombre, s.Entrenadores[j].Ranking))
		}
		if (s.Entrenadores[j].ID == resultado.IdEntrenador_1 || s.Entrenadores[j].ID == resultado.IdEntrenador_2) && s.Entrenadores[j].ID != resultado.IdGanador {
			s.Entrenadores[j].Ranking -= 5
			NotificarSNP("Ranking actualizado", fmt.Sprintf("Entrenador %s perdió y ahora tiene ranking %d", s.Entrenadores[j].Nombre, s.Entrenadores[j].Ranking))
		}
	}
	log.Printf("Resultado procesado correctamente: %+v", resultado)
	return nil
}

func (s *LcpServer) ConsumirResultados() {
	conn, err := amqp.Dial("amqp://guest:guest@10.35.168.24:5672/")
	if err != nil {
		log.Fatalf("Error al conectar a RabbitMQ: %v", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("Error al abrir canal: %v", err)
	}
	msgs, err := ch.Consume(
		"CDP",
		"",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("Error al consumir cola: %v", err)
	}
	go func() {
		for d := range msgs {
			var res AsignacionCombateResponse
			if err := json.Unmarshal(d.Body, &res); err != nil {
				log.Printf("[AUDITORIA] Error al deserializar resultado: %v", err)
				continue
			}
			if err := s.ProcesarResultadoCombate(res); err != nil {
				log.Printf("[AUDITORIA] Resultado no procesado: %v", err)
			}
		}
	}()
}

func (s *LcpServer) DesarrollarTorneo(r, t int) {
	connLcp, err := grpc.Dial("10.35.168.25:50053", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("Error al conectar al servidor de los gimnasios pokemon: %v", err)
	}
	defer connLcp.Close()
	gimnasioClient := pb.NewLcpServiceClient(connLcp)

	s.Regiones[r].Torneos[t].Estado = "Cerrado"
	torneo := &s.Regiones[r].Torneos[t]
	Combatientes := torneo.Combatientes
	rand.Shuffle(len(Combatientes), func(i, j int) {
		Combatientes[i], Combatientes[j] = Combatientes[j], Combatientes[i]
	})
	for len(Combatientes) > 1 {
		for i := 0; i < len(Combatientes); i += 2 {
			fmt.Printf("Combate entre %s y %s\n", Combatientes[i].Nombre, Combatientes[i+1].Nombre)
			nuevoCombateID := fmt.Sprintf("%s-%s-vs-%s", torneo.ID, Combatientes[i].ID, Combatientes[i+1].ID)
			torneo.Combates = append(torneo.Combates, nuevoCombateID)
			_, err := gimnasioClient.AsignarCombate(context.Background(), &pb.AsignacionCombateRequest{
				CombateId: nuevoCombateID,
				TorneoId:  torneo.ID,
				Entrenador_1: &pb.EntrenadorCombate{
					Id:      Combatientes[i].ID,
					Nombre:  Combatientes[i].Nombre,
					Ranking: int32(Combatientes[i].Ranking),
				},
				Entrenador_2: &pb.EntrenadorCombate{
					Id:      Combatientes[i+1].ID,
					Nombre:  Combatientes[i+1].Nombre,
					Ranking: int32(Combatientes[i+1].Ranking),
				},
				Region: s.Regiones[r].Nombre,
			})
			if err != nil {
				fmt.Println("Error al asignar combate:")
				continue
			}
		}
		break
	}
	torneo.Estado = "Finalizado"

	randRegion := rand.Intn(len(s.Regiones))
	nuevoTorneoID := fmt.Sprintf("%s-%d", s.Regiones[randRegion].Nombre, len(s.Regiones[randRegion].Torneos)+1)
	nuevoTorneo := Torneo{
		ID:           nuevoTorneoID,
		Estado:       "Activo",
		Region:       s.Regiones[randRegion].Nombre,
		Combatientes: []Entrenador{},
	}
	s.Regiones[randRegion].Torneos = append(s.Regiones[randRegion].Torneos, nuevoTorneo)
	fmt.Printf("Nuevo torneo creado: %s en la región %s\n", nuevoTorneoID, s.Regiones[randRegion].Nombre)
	NotificarSNP("Nuevo torneo", fmt.Sprintf("Torneo %s creado en la región %s", nuevoTorneoID, s.Regiones[randRegion].Nombre))
}

func (s *LcpServer) InscribirEntrenador(req *pb.InscripcionRequest) *pb.InscripcionResponse {
	var mensaje string

	for i := range s.Entrenadores {
		e := &s.Entrenadores[i]
		if e.ID == req.EntrenadorID {
			switch e.Estado {
			case "Suspendido":
				e.Suspension--
				if e.Suspension == 0 {
					e.Estado = "Activo"
				}
				mensaje = "Entrenador no inscrito por suspención, queda " + strconv.Itoa(e.Suspension) + " fechas de suspensión"
				NotificarSNP("Penalización", fmt.Sprintf("Entrenador %s está suspendido. Quedan %d fechas.", e.Nombre, e.Suspension))
			case "Activo":
				encontrado := false
				for r := range s.Regiones {
					for t := range s.Regiones[r].Torneos {
						if s.Regiones[r].Torneos[t].ID == req.TorneoID {
							encontrado = true
							if s.Regiones[r].Torneos[t].Estado == "Cerrado" {
								mensaje = "Entrenador no inscrito, el torneo está cerrado"
							} else if s.Regiones[r].Torneos[t].Estado == "Activo" {
								s.Regiones[r].Torneos[t].Combatientes = append(s.Regiones[r].Torneos[t].Combatientes, *e)
								e.Estado = "Inscrito"
								if len(s.Regiones[r].Torneos[t].Combatientes) == 8 {
									go s.DesarrollarTorneo(r, t)
								}
								mensaje = "Entrenador inscrito correctamente"
							}
						}
					}
				}
				if !encontrado {
					mensaje = "No se encontró el torneo"
				}
			case "Expulsado":
				mensaje = "Entrenador no inscrito por expulsión"
				NotificarSNP("Penalización", fmt.Sprintf("Entrenador %s fue expulsado.", e.Nombre))
			case "Inscrito":
				mensaje = "Entrenador inscrito en otro torneo"
			default:
				mensaje = "Entrenador error en inscribir al torneo"
			}
			return &pb.InscripcionResponse{
				Inscripcion: mensaje,
			}
		}
	}
	return &pb.InscripcionResponse{
		Inscripcion: "Entrenador no encontrado",
	}
}

func NewLcpServer() *LcpServer {
	rand.Seed(time.Now().UnixNano())
	regionesNombres := []string{"Kanto", "Johto", "Hoenn", "Sinnoh", "Unova", "Kalos", "Alola", "Galar", "Paldea"}
	var regiones []Region

	for _, nombre := range regionesNombres {
		var torneos []Torneo
		numTorneos := rand.Intn(4)
		for i := 0; i < numTorneos; i++ {
			torneoID := fmt.Sprintf("%s-%d", nombre, i+1)
			torneo := Torneo{
				ID:           torneoID,
				Estado:       "Activo",
				Region:       nombre,
				Combatientes: []Entrenador{},
			}
			torneos = append(torneos, torneo)
		}
		regiones = append(regiones, Region{
			Nombre:  nombre,
			Torneos: torneos,
		})
	}

	entrenadores := []Entrenador{}
	bytes, err := os.ReadFile("entrenadores_pequeno.json")
	if err == nil {
		_ = json.Unmarshal(bytes, &entrenadores)
	}

	return &LcpServer{
		Regiones:             regiones,
		Entrenadores:         entrenadores,
		ResultadosProcesados: make(map[string]bool),
	}
}

func (s *LcpServer) ConsultarTorneosDisponibles(ctx context.Context, req *pb.TorneosDisponiblesRequest) (*pb.TorneosDisponiblesResponse, error) {
	var torneos []*pb.TorneoInfo
	for _, region := range s.Regiones {
		for _, torneo := range region.Torneos {
			torneos = append(torneos, &pb.TorneoInfo{
				Region: region.Nombre,
				Id:     torneo.ID,
			})
		}
	}
	return &pb.TorneosDisponiblesResponse{
		Torneos: torneos,
	}, nil
}

func (s *LcpServer) InscribirseEnTorneo(ctx context.Context, req *pb.InscripcionRequest) (*pb.InscripcionResponse, error) {
	log.Printf("El entrenador %s, quiere inscribirse en el torneo %s", req.EntrenadorID, req.TorneoID)
	estado := s.InscribirEntrenador(req)
	if estado == nil {
		return estado, nil
	}
	return estado, nil
}

func PublishToQueue(queueName string, message []byte) error {
	conn, err := amqp.Dial("amqp://guest:guest@10.35.168.26:5672/")
	if err != nil {
		return err
	}
	defer conn.Close()

	channel, err := conn.Channel()
	if err != nil {
		return err
	}
	defer channel.Close()

	_, err = channel.QueueDeclare(
		queueName,
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return err
	}

	err = channel.Publish(
		"",
		queueName,
		false,
		false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        message,
		},
	)
	return err
}

func main() {
	serverLCP := NewLcpServer()
	serverLCP.ConsumirResultados()

	listener, err := net.Listen("tcp", ":50052")
	if err != nil {
		log.Fatalf("Error al iniciar el servidor: %v", err)
	}

	server := grpc.NewServer()
	pb.RegisterLcpServiceServer(server, serverLCP)
	reflection.Register(server)

	log.Println("Servidor de la Liga Central Pokemon en el puerto 50052")
	if err := server.Serve(listener); err != nil {
		log.Fatalf("Error al iniciar el servidor: %v", err)
	}
}
