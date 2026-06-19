#pragma once

#include <arpa/inet.h>
#include <cerrno>
#include <chrono>
#include <cmath>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <fcntl.h>
#include <filesystem>
#include <fstream>
#include <ifaddrs.h>
#include <iostream>
#include <memory>
#include <net/if.h>
#include <netinet/in.h>
#include <sstream>
#include <string>
#include <sys/ioctl.h>
#include <sys/socket.h>
#include <thread>
#include <unistd.h>

#define F(x) x
using __FlashStringHelper = char;
using String = std::string;

#ifndef PI
#define PI 3.14159265358979323846
#endif

constexpr int HIGH = 1;
constexpr int LOW = 0;
constexpr int INPUT_PULLUP = 2;
constexpr int OUTPUT = 1;
constexpr int LED_BUILTIN = 25;
constexpr int NEOPIXEL = 11;
constexpr int NEO_GRB = 0;
constexpr int NEO_KHZ800 = 0;
constexpr int WL_NO_MODULE = 255;
constexpr int WL_CONNECTED = 3;

inline auto g_simStart = std::chrono::steady_clock::now();

inline uint32_t millis() {
  auto elapsed = std::chrono::steady_clock::now() - g_simStart;
  return (uint32_t)std::chrono::duration_cast<std::chrono::milliseconds>(elapsed).count();
}

inline void delay(unsigned long ms) {
  std::this_thread::sleep_for(std::chrono::milliseconds(ms));
}

inline void pinMode(int, int) {}
inline void digitalWrite(int, int) {}
inline int digitalRead(int) { return HIGH; }

class SimPrint {
public:
  virtual ~SimPrint() = default;
  virtual size_t write(const uint8_t* data, size_t len) = 0;

  size_t write(const char* s) { return write((const uint8_t*)s, std::strlen(s)); }
  size_t write(uint8_t b) { return write(&b, 1); }

  template <class T>
  void print(const T& value) {
    std::ostringstream out;
    out << value;
    const std::string s = out.str();
    write((const uint8_t*)s.data(), s.size());
  }

  void print(const char* s) {
    if (s) write((const uint8_t*)s, std::strlen(s));
  }

  void print(char ch) {
    write((const uint8_t*)&ch, 1);
  }

  template <class T>
  void println(const T& value) {
    print(value);
    print('\n');
  }

  void println() { print('\n'); }
};

class SerialPort : public SimPrint {
public:
  void begin(unsigned long) {}
  explicit operator bool() const { return true; }

  size_t write(const uint8_t* data, size_t len) override {
    std::cout.write((const char*)data, (std::streamsize)len);
    std::cout.flush();
    return len;
  }
};

inline SerialPort Serial;
inline SerialPort Serial2;

class File : public SimPrint {
public:
  File() = default;
  File(const std::filesystem::path& path, std::ios::openmode mode)
      : path_(path), stream_(std::make_shared<std::fstream>(path, mode | std::ios::binary)) {}

  explicit operator bool() const {
    return stream_ && stream_->is_open() && stream_->good();
  }

  int available() {
    if (!stream_ || !stream_->is_open()) return 0;
    auto current = stream_->tellg();
    if (current < 0) return 0;
    stream_->seekg(0, std::ios::end);
    auto end = stream_->tellg();
    stream_->seekg(current);
    return end > current ? (int)(end - current) : 0;
  }

  int read() {
    char ch;
    if (!stream_ || !stream_->read(&ch, 1)) return -1;
    return (uint8_t)ch;
  }

  int read(uint8_t* buf, size_t len) {
    if (!stream_) return -1;
    stream_->read((char*)buf, (std::streamsize)len);
    return (int)stream_->gcount();
  }

  String readStringUntil(char delim) {
    std::string out;
    if (!stream_) return out;
    std::getline(*stream_, out, delim);
    return out;
  }

  size_t write(const uint8_t* data, size_t len) override {
    if (!stream_ || !stream_->is_open()) return 0;
    stream_->write((const char*)data, (std::streamsize)len);
    return stream_->good() ? len : 0;
  }

  long size() {
    if (path_.empty() || !std::filesystem::exists(path_)) return 0;
    return (long)std::filesystem::file_size(path_);
  }

  void close() {
    if (stream_ && stream_->is_open()) stream_->close();
  }

private:
  std::filesystem::path path_;
  std::shared_ptr<std::fstream> stream_;
};

class LittleFSClass {
public:
  bool begin() {
    std::error_code ec;
    std::filesystem::create_directories(root(), ec);
    return !ec;
  }

  void format() {
    std::error_code ec;
    std::filesystem::remove_all(root(), ec);
    std::filesystem::create_directories(root(), ec);
  }

  File open(const char* path, const char* mode) {
    std::filesystem::path full = resolve(path);
    std::filesystem::create_directories(full.parent_path());
    std::ios::openmode flags = std::ios::binary;
    if (std::strchr(mode, 'r')) flags |= std::ios::in;
    if (std::strchr(mode, 'w')) flags |= std::ios::out | std::ios::trunc;
    if (std::strchr(mode, 'a')) flags |= std::ios::out | std::ios::app;
    return File(full, flags);
  }

  bool exists(const char* path) {
    return std::filesystem::exists(resolve(path));
  }

  bool remove(const char* path) {
    std::error_code ec;
    return std::filesystem::remove(resolve(path), ec);
  }

  bool rename(const char* from, const char* to) {
    std::error_code ec;
    std::filesystem::rename(resolve(from), resolve(to), ec);
    return !ec;
  }

  static std::filesystem::path root() {
    const char* env = std::getenv("ROOTAIKA_SIM_FS");
    return env && *env ? std::filesystem::path(env) : std::filesystem::path("simdata");
  }

private:
  static std::filesystem::path resolve(const char* path) {
    std::string p = path ? path : "";
    while (!p.empty() && p.front() == '/') p.erase(p.begin());
    return root() / p;
  }
};

inline LittleFSClass LittleFS;

class WiFiClient : public SimPrint {
public:
  WiFiClient() = default;
  explicit WiFiClient(int fd) : fd_(fd) {}
  WiFiClient(const WiFiClient&) = delete;
  WiFiClient& operator=(const WiFiClient&) = delete;
  WiFiClient(WiFiClient&& other) noexcept { fd_ = other.fd_; other.fd_ = -1; }
  WiFiClient& operator=(WiFiClient&& other) noexcept {
    if (this != &other) {
      stop();
      fd_ = other.fd_;
      other.fd_ = -1;
    }
    return *this;
  }
  ~WiFiClient() { stop(); }

  explicit operator bool() const { return fd_ >= 0; }
  bool connected() const { return fd_ >= 0; }

  int available() {
    if (fd_ < 0) return 0;
    int count = 0;
    if (ioctl(fd_, FIONREAD, &count) < 0) return 0;
    return count;
  }

  int read() {
    uint8_t ch;
    ssize_t n = ::recv(fd_, &ch, 1, 0);
    return n == 1 ? ch : -1;
  }

  int read(uint8_t* buf, size_t len) {
    ssize_t n = ::recv(fd_, buf, len, 0);
    return n > 0 ? (int)n : -1;
  }

  size_t write(const uint8_t* data, size_t len) override {
    if (fd_ < 0) return 0;
    size_t sent = 0;
    while (sent < len) {
      ssize_t n = ::send(fd_, data + sent, len - sent, 0);
      if (n <= 0) break;
      sent += (size_t)n;
    }
    return sent;
  }

  size_t write(File& file) {
    uint8_t buf[2048];
    size_t total = 0;
    while (file.available() > 0) {
      int n = file.read(buf, sizeof(buf));
      if (n <= 0) break;
      total += write(buf, (size_t)n);
    }
    return total;
  }

  void flush() {}

  void stop() {
    if (fd_ >= 0) {
      ::shutdown(fd_, SHUT_RDWR);
      ::close(fd_);
      fd_ = -1;
    }
  }

private:
  int fd_ = -1;
};

class WiFiServer {
public:
  explicit WiFiServer(int port) : firmwarePort_(port) {}

  void begin(int backlog = 5, uint16_t = 0) {
    if (listenFd_ >= 0) return;
    listenFd_ = ::socket(AF_INET, SOCK_STREAM, 0);
    if (listenFd_ < 0) {
      perror("socket");
      std::exit(1);
    }
    int yes = 1;
    setsockopt(listenFd_, SOL_SOCKET, SO_REUSEADDR, &yes, sizeof(yes));
    sockaddr_in addr{};
    addr.sin_family = AF_INET;
    addr.sin_addr.s_addr = htonl(INADDR_ANY);
    addr.sin_port = htons(simPort());
    if (::bind(listenFd_, (sockaddr*)&addr, sizeof(addr)) < 0) {
      perror("bind");
      std::exit(1);
    }
    if (::listen(listenFd_, backlog) < 0) {
      perror("listen");
      std::exit(1);
    }
    fcntl(listenFd_, F_SETFL, fcntl(listenFd_, F_GETFL, 0) | O_NONBLOCK);
    std::cout << "[sim] firmware port " << firmwarePort_ << " mapped to http://0.0.0.0:" << simPort() << "/\n";
    printClientUrls();
  }

  WiFiClient available() {
    if (listenFd_ < 0) return WiFiClient();
    int fd = ::accept(listenFd_, nullptr, nullptr);
    if (fd < 0) return WiFiClient();
    return WiFiClient(fd);
  }

private:
  static void printClientUrls() {
    std::cout << "[sim] client URL on this Mac: http://127.0.0.1:" << simPort() << "/\n";

    ifaddrs* addrs = nullptr;
    if (getifaddrs(&addrs) != 0) return;
    bool printedLan = false;
    for (ifaddrs* it = addrs; it; it = it->ifa_next) {
      if (!it->ifa_addr || it->ifa_addr->sa_family != AF_INET) continue;
      if ((it->ifa_flags & IFF_LOOPBACK) || !(it->ifa_flags & IFF_UP)) continue;

      char ip[INET_ADDRSTRLEN];
      auto* in = (sockaddr_in*)it->ifa_addr;
      if (!inet_ntop(AF_INET, &in->sin_addr, ip, sizeof(ip))) continue;
      std::cout << "[sim] client URL from VM/LAN: http://" << ip << ":" << simPort() << "/"
                << " (" << it->ifa_name << ")\n";
      printedLan = true;
    }
    freeifaddrs(addrs);

    if (!printedLan)
      std::cout << "[sim] no non-loopback IPv4 address found for VM/LAN clients\n";
  }

  static int simPort() {
    const char* env = std::getenv("ROOTAIKA_SIM_PORT");
    if (env && *env) return std::atoi(env);
    return 8080;
  }

  int firmwarePort_;
  int listenFd_ = -1;
};

class WiFiClass {
public:
  void init(SerialPort&) {}
  int status() const { return WL_CONNECTED; }
  void begin(const char*, const char*) {}
  bool startMDNS(const char*, const char*, int) { return true; }
  void sntp(const char*, const char*) {}
  unsigned long getTime() const { return (unsigned long)std::time(nullptr); }
  const char* localIP() const { return "127.0.0.1"; }
};

inline WiFiClass WiFi;

class Adafruit_NeoPixel {
public:
  Adafruit_NeoPixel(int, int, int) {}
  void begin() {}
  void setBrightness(int) {}
  void clear() {}
  uint32_t Color(uint8_t r, uint8_t g, uint8_t b) {
    return ((uint32_t)r << 16) | ((uint32_t)g << 8) | b;
  }
  void setPixelColor(int, uint32_t) {}
  void show() {}
};
