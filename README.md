# GoEmitter


## Introducción

**GoEmitter** es una librería de Go que proporciona un sistema de eventos robusto, concurrente y rico en funcionalidades. Está diseñada para manejar eventos de manera síncrona y asíncrona con soporte para pools de workers, recolección de métricas y operaciones type-safe a través de métodos genéricos.

## Características Principales

- **Concurrencia Segura**: Todas las operaciones son thread-safe
- **Emisión Asíncrona y Síncrona**: Flexibilidad en el manejo de eventos
- **Pool de Workers**: Control sobre la concurrencia con límites configurables
- **Métricas en Tiempo Real**: Monitoreo del rendimiento y uso
- **Type Safety**: Métodos genéricos para operaciones con tipos seguros
- **Manejo de Pánicos**: Recuperación automática de errores en callbacks
- **Hooks de Emisión**: Lógica personalizada antes de cada evento
- **Soporte de Contexto**: Cancelación de eventos con `context.Context`

## Instalación


```bash
go get github.com/abdielbs7/goemitter
```

## Uso Básico

## Creación de un Emitter

```go
package main

import (     
	"fmt"    
	"github.com/abdielbs7/goemitter" 
)

func main() {     
	// Crear emitter con configuración por defecto    
	emitter := goemitter.New()         
	// O con opciones personalizadas    
	emitter := goemitter.NewWithOptions( 
		  goemitter.WithMaxWorkers(10),        
		  goemitter.WithMetrics(true),    
	)
}
```


## Registrar Listeners

```go
// Listener persistente 
listenerID, err := emitter.On("user.created", func(args ...interface{}) {
	fmt.Printf("Usuario creado: %v\n", args[0]) 
}) 
// Listener de una sola vez 
onceID, err := emitter.Once("app.started", func(args ...interface{}) {
	fmt.Println("Aplicación iniciada") 
})
```

## Emitir Eventos

```go
// Emisión asíncrona 
err := emitter.Emit("user.created", "Juan Pérez", 25) 
// Emisión síncrona 
err := emitter.EmitSync("user.created", "María García", 30) 
// Esperrar a que terminen todos los callbacks asíncronos 
emitter.WaitAsync()
```

## Configuración Avanzada

## Opciones de Configuración

```go
emitter := goemitter.NewWithOptions(     
	// Limitar workers concurrentes    
	goemitter.WithMaxWorkers(5),         
	// Manejador de pánicos personalizado    
	goemitter.WithPanicHandler(func(err interface{}) {        
		log.Printf("Pánico recuperado: %v", err)    
	}),        
	// Logger personalizado    
	goemitter.WithLogger(myLogger),         
	// Habilitar/deshabilitar métricas    
	goemitter.WithMetrics(true), 
)
```

## Implementar Logger Personalizado

```go
type MyLogger struct{} 

func (l *MyLogger) Error( msg string, args ...interface{}) {
	log.Printf("[ERROR] "+msg, args...) 
} 

func (l *MyLogger) Warn(msg string, args ...interface{}) {
	log.Printf("[WARN] "+msg, args...) 
} 

func (l *MyLogger) Info(msg string, args ...interface{}) {
	log.Printf("[INFO] "+msg, args...) 
}
```


## Funcionalidades Avanzadas

## Operaciones Type-Safe


```go
// Registrar listener con tipo específico 
type User struct {
	Name string
	Age  int
} 
listenerID, err := goemitter.OnTyped (emitter, "user.created", func (user User) {
	fmt.Printf("Usuario: %s, Edad: %d\n", user.Name, user.Age) 
}) 
// Emitir evento con tipo específico 
user := User { Name: "Ana", Age: 28 } 
err := goemitter.EmitTyped (emitter, "user.created", user)
```

## Manejo con Contexto

```go
ctx, cancel := context.WithTimeout (context.Background(), 5*time.Second) 
defer cancel() 
// Emitir con contexto (cancelable) 
err := emitter.EmitWithContext (ctx, "long.process", data)
```

## Hooks de Emisión

```go
// Agregar hook que se ejecuta antes de cada emisión
emitter.AddEmitHook( 
	func (event string, listenerCount int) {
		fmt.Printf("Emitiendo evento '%s' a %d listeners\n", event, listenerCount)
	}
)
```

## Métricas y Monitoreo

```go
// Obtener métricas actuales 
metrics := emitter.GetMetrics() 
fmt.Printf("Eventos emitidos: %d\n", metrics.EventsEmitted) 
fmt.Printf("Listeners ejecutados: %d\n", metrics.ListenersExecuted)
fmt.Printf("Pánicos recuperados: %d\n", metrics.PanicsRecovered)
fmt.Printf("Latencia promedio: %v\n", metrics.AverageLatency) 
fmt.Printf("Workers activos: %d\n", metrics.ActiveWorkers)
```

## Ejemplos Prácticos

## Sistema de Notificaciones

```go
package main

import (
	"fmt"
	"github.com/abdielbs7/goemitter" 
)

type Notification struct {
	Title   string    
	Message string    
	UserID  string 
} 

func main() {
	emitter := goemitter.NewWithOptions(
		goemitter.WithMaxWorkers(3),
		goemitter.WithMetrics(true),
	)
	// Listener para notificaciones por email
	emitter.On ("notification.send", func(args ...interface{}) {
		notif := args[0].(Notification)
		fmt.Printf("📧 Email enviado a %s: %s\n", notif.UserID, notif.Title)
	})
	// Listener para notificaciones push
	emitter.On ("notification.send", func(args ...interface{}) {
		notif := args[0].(Notification)
		fmt.Printf("📱 Push enviado a %s: %s\n", notif.UserID, notif.Title)
	})
	// Listener para logging
	emitter.On("notification.send", func(args ...interface{}) {
		notif := args[0].(Notification)
		fmt.Printf("📝 Log: Notificación enviada a %s\n", notif.UserID)
	})
	// Emitir notificación
	notification := Notification { 
		Title: "Bienvenido", 
		Message: "Gracias por registrarte", 
		UserID: "user123", 
	}
	emitter.Emit("notification.send", notification)
	emitter.WaitAsync()
	// Mostrar métricas
	metrics := emitter.GetMetrics()
	fmt.Printf( 
		"\nMétricas: %d eventos, %d listeners ejecutados\n",
		metrics.EventsEmitted, metrics.ListenersExecuted
	) 
}
```

## Sistema de Auditoría


```go
package main

import (
	"fmt"
	"time"
	"github.com/abdielbs7/goemitter"
)

type AuditEvent struct {
	Action    string    
	UserID    string    
	Resource  string    
	Timestamp time.Time
}
func main() {
	emitter := goemitter.NewWithOptions(
		goemitter.WithMaxWorkers(2),
	)
	// Hook para logging automático
	emitter.AddEmitHook( func (event string, listenerCount int) {
		fmt.Printf("[AUDIT] Evento '%s' -> %d listeners\n", event, listenerCount)
	})
	// Listener para base de datos
	emitter.On ("audit.log", func(args ...interface{}) {
		event := args[0].(AuditEvent)
		fmt.Printf (
			"💾 BD: %s realizó %s en %s\n", 
			event.UserID, 
			event.Action, 
			event.Resource
		)
	})
	// Listener para archivo de log
	emitter.On ("audit.log", func(args ...interface{}) {
		event := args[0].(AuditEvent)
		fmt.Printf(
			"📄 FILE: [%s] %s - %s - %s\n", 
			event.Timestamp.Format("2006-01-02 15:04:05"),
			event.UserID,
			event.Action,
			event.Resource
		)
	})
	// Emitir evento de auditoría
	auditEvent := AuditEvent {
		Action:    "DELETE",
		UserID:    "admin",
		Resource:  "users/123",
		Timestamp: time.Now(),
	}
	emitter.Emit("audit.log", auditEvent)
	emitter.WaitAsync() 
}
```

## Referencia de API

## Tipos Principales

## GoEmitter

Estructura principal que implementa todas las funcionalidades del emitter.
## EmitterConfig

Configuración para el emitter:
- `MaxWorkers`: Número máximo de workers concurrentes (0 = ilimitado)
- `PanicHandler`: Manejador de pánicos personalizado
- `Logger`: Interfaz de logging
- `BufferSize`: Tamaño del buffer para canales
- `EnableMetrics`: Habilitar recolección de métricas

## EmitterMetrics

Métricas del emitter:
- `EventsEmitted`: Total de eventos emitidos
- `ListenersExecuted`: Total de listeners ejecutados
- `PanicsRecovered`: Total de pánicos recuperados
- `AverageLatency`: Latencia promedio de ejecución
- `ActiveWorkers`: Workers activos actualmente

## Métodos Principales

## Creación

- `New()`: Crear emitter con configuración por defecto
- `NewWithOptions(opts ...EmitterOption)`: Crear con opciones personalizadas

## Registro de Listeners

- `On(event string, callback func(...interface{})) (uint64, error)`: Listener persistente
- `Once(event string, callback func(...interface{})) (uint64, error)`: Listener de una vez
- `OnTyped[T](emitter, event string, callback func(T)) (uint64, error)`: Listener con tipo específico

## Emisión de Eventos

- `Emit(event string, args ...interface{}) error`: Emisión asíncrona
- `EmitSync(event string, args ...interface{}) error`: Emisión síncrona
- `EmitWithContext(ctx context.Context, event string, args ...interface{}) error`: Emisión con contexto
- `EmitTyped[T](emitter, event string, data T) error`: Emisión con tipo específico

## Gestión de Listeners

- `Off(event string, listenerID uint64) error`: Remover listener específico
- `Clear(event string) error`: Remover todos los listeners de un evento
- `ClearAll() error`: Remover todos los eventos y listeners

## Información y Métricas

- `ListenerCount(event string) int`: Número de listeners para un evento
- `Events() []string`: Lista de eventos registrados
- `GetMetrics() EmitterMetrics`: Obtener métricas actuales
- `WaitAsync()`: Esperar a que terminen todos los callbacks asíncronos

## Funcionalidades Avanzadas

- `AddEmitHook(hook EmitHook)`: Agregar hook de emisión

## Opciones de Configuración

- `WithMaxWorkers(max int)`: Configurar máximo de workers
- `WithPanicHandler(handler func(interface{}))`: Configurar manejador de pánicos
- `WithLogger(logger Logger)`: Configurar logger personalizado
- `WithMetrics(enabled bool)`: Habilitar/deshabilitar métricas

## Mejores Prácticas

## 1. Manejo de Errores

```go
listenerID, err := emitter.On ("event", callback) 
if err != nil {
	log.Printf ("Error registrando listener: %v", err)
	return
}
```

## 2. Limpieza de Recursos

```go
// Siempre limpiar listeners cuando no sean necesarios 
defer emitter.Off("event", listenerID) 
// O limpiar todos los eventos al finalizar 
defer emitter.ClearAll()
```

## 3. Uso de Contextos

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second) 
defer cancel() 
if err := emitter.EmitWithContext(ctx, "event", data); err != nil { 
	if errors.Is(err, context.DeadlineExceeded) { 
		log.Println("Timeout en emisión de evento")
	}
}
```

## 4. Monitoreo con Métricas

```go
// Verificar métricas regularmente 
go func() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		metrics := emitter.GetMetrics()
		if metrics.PanicsRecovered > 0 {
			log.Printf(
				"ADVERTENCIA: %d pánicos recuperados", metrics.PanicsRecovered
			)
		}
	}
}()
```

## 5. Configuración según Caso de Uso

```go
// Para alta concurrencia
emitter := goemitter.NewWithOptions(
	goemitter.WithMaxWorkers(runtime.NumCPU()),
	goemitter.WithMetrics(true),
) 
// Para máximo rendimiento 
emitter := goemitter.NewWithOptions(
	goemitter.WithMaxWorkers(0), // Sin límite
	goemitter.WithMetrics(false), // Sin métricas 
)
```

## Consideraciones de Rendimiento

1. **Workers**: Ajustar `MaxWorkers` según la carga esperada
2. **Métricas**: Deshabilitar si no son necesarias para mayor rendimiento
3. **Contextos**: Usar timeouts apropiados para evitar bloqueos
4. **Limpieza**: Remover listeners no utilizados para liberar memoria

## Conclusión

GoEmitter es una librería completa y flexible para el manejo de eventos en Go. Su diseño concurrente y rico en funcionalidades la hace ideal para aplicaciones que requieren un sistema de eventos robusto y escalable. La combinación de operaciones type-safe, métricas en tiempo real y configuración flexible la convierte en una herramienta poderosa para desarrolladores Go.
