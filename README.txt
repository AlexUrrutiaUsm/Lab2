Laboratorio 2 Sistemas Distribuidos.
Nombre: Alex Urrutia Rol: 202073565-4 
Nombre: Ariel Pulgar Rol: 202073023-7 

Para esta Tarea se realizaron las siguientes suposiciones y consideraciones que no son especificadas o no quedan explícitas en el enunciado:
-Existen "entrenadores.json" en lcp
-Todas las entidades se encuentran en todas las maquinas virtuales
-La entidad lcp se encuentra en máquina virtual 13.
-La entidad entrenador-cdp se encuentra en máquina virtual 14.
-La entidad gimnasio se encuentra en máquina virtual 15.
-La entidad snp se encuentra en máquina virtual 16.

Ejecución del programa:
Para ejecutar el programa, es necesario inicializar los archivos "mains" de cada entidad, estas se ejecutaran de la siguiente forma:

1. Dirigirse a la carpeta de la entidad en la maquina virtual que se inicializara actualmente.
2. Escribir sudo make docker-[Nombre de la entidad a inicializar] en la terminal.
3. Repetir hasta que se ejecute los pasos 1 y 2 en las 4 maquinas.
4. Escribir sudo docker ps para visualizar la id del container.
5. Escribir sudo docker logs [ID de la container] para visualizar los prints del sistema.
6. Escribir sudo docker stop [ID de la container] , para parar la ejecución del programa, repetir en las 4 maquinas.
