# Reglas de la Loba (Loba de Menos)

Juego de cartas rioplatense de la familia del rummy. Esta variante es la **Loba de Menos**: gana quien acumule menos puntos de penalización.

---

## Objetivo

Deshacerse de todas las cartas de la mano formando combinaciones válidas (bajadas) en la mesa. Cuando un jugador agota su mano, la ronda termina. Los demás reciben puntos de penalización por las cartas que les quedan. Gana quien tenga el **menor puntaje acumulado** cuando algún jugador supera el límite.

---

## Mazos y cartas

- **2 mazos** estándar de 52 cartas (104 cartas) más **4 comodines** = **108 cartas en total**.
- Los comodines son salvajes: reemplazan cualquier carta en una escalera.

---

## Reparto

- Se reparten **9 cartas** a cada jugador (en sentido horario).
- La siguiente carta se coloca boca arriba para iniciar el **pozo** (pila de descarte).
- El resto forma el **mazo** (pila boca abajo).

---

## Turno

Cada turno tiene cuatro fases, en orden:

### 1. Robar

El jugador debe tomar exactamente una carta:

- **Del mazo**: toma la carta de arriba (boca abajo) y la agrega a su mano. No hay restricciones.
- **Del pozo**: toma la carta visible del tope del pozo, pero **solo si puede usarla inmediatamente** — únicamente si puede formar con ella una **bajada nueva** desde su mano en ese momento (no alcanza con poder agregarla a una combinación ya existente en la mesa). La carta tomada del pozo **debe jugarse en ese mismo turno**; no se puede guardar en la mano ni descartar.

> **Esta implementación**: se aplica estrictamente la restricción de uso inmediato al tomar del pozo.

### 2. Bajar (opcional)

El jugador puede bajar una o más combinaciones válidas de su mano a la mesa.

### 3. Agregar (opcional)

Si el jugador **ya bajó al menos una combinación** en esta o en rondas anteriores durante este turno, puede agregar cartas de su mano a cualquier combinación en la mesa (propia o ajena), de a una carta por vez.

### 4. Descartar

El jugador descarta una carta boca arriba al pozo para terminar su turno.

- **No se puede descartar un comodín** mientras haya otra carta en la mano. El comodín debe jugarse en una combinación. Excepción: si la mano consiste únicamente en comodines (situación muy rara), se permite descartarlo para terminar el turno.

---

## Combinaciones válidas

### Pierna

Tres cartas del **mismo valor** en **palos diferentes**.

- Al momento de la creación: exactamente 3 cartas, todos los palos deben ser distintos entre sí, sin comodines.
- **No se admiten comodines** en ninguna posición de la pierna (ni al crearla ni al agregar).
- Después de bajarla, se pueden agregar más cartas del mismo valor (cualquier palo, incluyendo duplicados de palo, dado que hay dos mazos).
- Máximo 8 cartas por pierna (4 palos × 2 mazos).

Ejemplos válidos: `7♠ 7♥ 7♦` | `K♣ K♠ K♦` | `A♠ A♥ A♣`

### Escalera

Tres o más cartas del **mismo palo** en **secuencia consecutiva de valores**.

- El as puede ser **bajo** (A-2-3) o **alto** (Q-K-A), pero **no puede ser simultáneamente alto y bajo** en la misma escalera (no se permite K-A-2).
- Se admite **como máximo 1 comodín** por escalera.
- El comodín ocupa una posición fija dentro de la secuencia; **no puede moverse** una vez bajado (ver sección Comodines).

Ejemplos válidos: `5♣ 6♣ 7♣` | `A♦ 2♦ 3♦` | `Q♥ K♥ A♥` | `5♠ ★JKR 7♠`

---

## Comodines

- Solo se permiten en **escaleras**, nunca en piernas.
- **Máximo 1 comodín** por escalera.
- Al guardar una escalera, el comodín queda fijado en la posición de la secuencia que ocupa y **no se puede desplazar ni canjear**.
- **No se puede descartar un comodín** salvo que sea la única carta que queda en la mano (ver sección Descartar).

> **Variante conocida**: algunas versiones permiten hasta 2 comodines por escalera, o permiten canjear el comodín por la carta real que representa. **Esta implementación** no soporta ninguna de las dos variantes: máximo 1 comodín, sin canje.

> **Variante conocida**: algunas versiones permiten mover el comodín de un extremo al otro de la escalera al agregar. **Esta implementación** no soporta el movimiento del comodín una vez bajado.

---

## Cierre de la mano

La ronda termina cuando un jugador logra **agotar su mano** por completo (ya sea mediante una bajada o un descarte). En ese momento se puntúan las manos restantes.

---

## Puntuación

Los jugadores con cartas en la mano al cierre reciben puntos de penalización:

| Carta          | Puntos de penalización |
|----------------|------------------------|
| Comodín        | 25                     |
| As             | 15                     |
| J, Q, K, 10    | 10                     |
| 2 a 9          | valor nominal          |

El jugador que cierra recibe **0 puntos** en esa ronda.

### Cerrar de mano (menos diez)

**Regla oficial (Pagat.com, Loba de Menos):** "If you win a round by putting down all of your cards at the same time (forming your own piernas or escaleras or adding to those of other players), without having previously put down any cards in that round, your cumulative score is reduced by 10 points."

**Interpretación en esta implementación:** el turno de Loba permite bajar varias combinaciones y agregar cartas antes de descartar, por lo que "al mismo tiempo" se interpreta de la siguiente manera: el jugador que cierra gana el bono de −10 si y solo si **todas sus jugadas en la mesa** (bajadas y agregados) ocurrieron dentro de su **turno final** — es decir, no había bajado ni agregado ninguna carta en ningún turno anterior de la misma ronda.

**Seguimiento:** se registra internamente el número de turno del primer movimiento en la mesa de cada jugador. Al cerrar, si ese número coincide con el turno actual, el bono aplica.

**Efecto en el puntaje:**

- El puntaje acumulado del jugador que cierra se reduce en 10 puntos.
- El puntaje de la ronda de ese jugador se registra como **−10** (en lugar de 0) para que la suma por columna de la tabla de puntajes coincida con los totales acumulados.
- Los **totales negativos están permitidos**: un jugador puede quedar con un puntaje total menor a cero sin que eso afecte la lógica del juego (el límite de 101 se controla solo con los valores acumulados > 101).
- El evento de cierre muestra: *"¡[nombre] cerró de mano! −10 puntos."*
- En el resumen de ronda, el bloque del ganador indica: *"ganó la mano — ¡de mano! −10 pts"*.
- La tabla de puntajes muestra −10 en la celda correspondiente a esa ronda.

**Fuente:** [Pagat.com — Rules of Card Games: Loba](https://www.pagat.com/rummy/loba.html)

> **Nota de variantes**: otras fuentes asignan 10 puntos al As. **Esta implementación** usa 15 puntos para el As y 25 para el comodín, acorde al sistema más extendido en Argentina.

---

## Fin de la partida

- Cuando el puntaje acumulado de **algún jugador supera los 101 puntos**, la partida termina al finalizar esa ronda.
- El jugador con el **menor puntaje total** gana.
- En caso de empate, ganan todos los jugadores empatados.

> **Esta implementación**: límite en 101 puntos (superarlo desencadena el fin), sin reenganche.

> **Variante conocida**: algunas versiones permiten hasta dos "reenganches" (el jugador eliminado paga una penalización y vuelve). **Esta implementación** no soporta reenganches.

---

## Resumen de decisiones de implementación

| Regla | Decisión adoptada |
|---|---|
| Mazos | 2 mazos + 4 comodines (108 cartas) |
| Cartas repartidas | 9 por jugador |
| Tomar del pozo | Solo si se usa en ese turno |
| Pierna | Exactamente 3 cartas al crear, sin comodines |
| Comodín en pierna | No permitido |
| Comodines por escalera | Máximo 1 |
| Movimiento de comodín | No soportado |
| Canje de comodín | No soportado |
| Descartar comodín | Prohibido (excepción: única carta en mano) |
| Puntuación del As | 15 puntos |
| Puntuación del comodín | 25 puntos |
| Límite de puntos | 101 (superarlo termina el juego) |
| Cerrar de mano | −10 pts al total acumulado; totales negativos permitidos |
| Reenganches | No soportados |

---

## Desconexiones y reconexión

### Turno automático al desconectarse

Si un jugador pierde la conexión durante la partida, el servidor juega sus
turnos automáticamente mientras está desconectado: roba del mazo y descarta
la carta robada (con ~1 segundo de pausa para que los demás vean el flujo).
Esto ocurre **cada vez** que le toca el turno — no solo una vez. Si hay
varios jugadores desconectados consecutivos, los turnos se encadenan.
El registro de eventos muestra un aviso del estilo *"fulano está
desconectado — turno automático"* para cada turno auto-jugado.

### Reconexión con el selector de lugar

Cuando un jugador se desconecta y vuelve a unirse, el juego muestra una
pantalla de selección de lugar en lugar de volver directamente a la mesa.
El nombre no importa en esta etapa — el lugar define la identidad del jugador
que regresa.

```sh
./play.sh join <dirección>
# o bien: ./loba join <dirección>
```

1. El servidor detecta que la partida ya comenzó y envía la lista de lugares
   disponibles (jugadores desconectados).
2. El cliente muestra la pantalla **"Elegí tu lugar para volver a la partida"**
   con nombre, cantidad de cartas y puntaje de cada lugar libre.
3. El jugador elige con `↑ ↓` y confirma con `Enter`.
4. El servidor reestablece la conexión en ese lugar: la mano y el puntaje
   se conservan íntegramente. Los demás jugadores reciben el aviso
   *"Fulano se reconectó."*

Si el jugador se reconecta justo antes de que se ejecute un turno automático,
la reconexión tiene prioridad: el servidor verifica el estado de conexión
antes de auto-jugar.

Si dos jugadores intentan reclamar el mismo lugar al mismo tiempo, el primero
en llegar se queda con él. El segundo recibe un error y una lista actualizada
de lugares disponibles (o un mensaje de que ya no quedan lugares libres).

**Errores posibles:**

| Situación | Mensaje |
|-----------|---------|
| Nadie está desconectado | "la partida ya comenzó y no hay lugares libres" |
| Lugar reclamado por otra persona | error + lista actualizada |

---

## Fuentes

- [Pagat.com — Rules of Card Games: Loba](https://www.pagat.com/rummy/loba.html)
- [The Rummy Rulebook — Loba de Menos](https://www.rummyrulebook.com/pages/loba-de-menos/)
- [CardRules+ — Cómo jugar a Loba](https://cardrulesplus.com/games/loba/)
