


package main

import (
    "fmt"

    "golang.org/x/crypto/bcrypt"
)

func main() {
    password := "DEIN_PASSWORT_HIER"

    hashBytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
    if err != nil {
        panic(err)
    }

    fmt.Println(string(hashBytes))
}



