#define ROOTAIKA_HOST_SIM 1

#include "arduino_compat.h"
#include "../RootaikaServer/RootaikaServer.ino"

int main() {
  setup();
  for (;;) {
    loop();
    delay(1);
  }
}
