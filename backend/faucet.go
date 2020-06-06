package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"errors"

	"github.com/dpapathanasiou/go-recaptcha"
	"github.com/joho/godotenv"
	"github.com/tomasen/realip"
)

type ErrorResponse struct {
	Status  bool        `json:"status"`
	Message string      `json:"message"`
	Error   interface{} `json:"error"`
}

type SuccessResponse struct {
	Status bool        `json:"status"`
	Data   interface{} `json:"data"`
}

type AccountQueryRes struct {
	Type  string `json:"type"`
	Value Value  `json:"value"`
}

type Value struct {
	Address        string  `json:"address"`
	Coins          []Coins `json:"coins"`
	Public_key     string  `json:"public_key"`
	Account_number int     `json:"account_number"`
	Sequence       int     `json:"sequence"`
}

type Coins struct {
	Denom  string `json:"denom"`
	Amount string `json:"amount"`
}

var chain string
var recaptchaSecretKey string
var amountFaucet string
var amountSteak string
var key string
var pass string
var node string
var publicUrl string
var maxTokens float64

const ADDR_LENGTH int = 44

type claim_struct struct {
	Address  string `json:"address"`
	Response string `json:"response"`
}

func getEnv(key string) string {
	if value, ok := os.LookupEnv(key); ok {
		fmt.Println(key, "=", value)
		return value
	} else {
		log.Fatal("Error loading environment variable: ", key)
		return ""
	}
}

func main() {
	err := godotenv.Load(".env.local", ".env")
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	chain = getEnv("FAUCET_CHAIN")
	recaptchaSecretKey = getEnv("FAUCET_RECAPTCHA_SECRET_KEY")
	amountFaucet = getEnv("FAUCET_AMOUNT_FAUCET")
	amountSteak = getEnv("FAUCET_AMOUNT_STEAK")
	key = getEnv("FAUCET_KEY")
	pass = getEnv("FAUCET_PASS")
	node = getEnv("FAUCET_NODE")
	publicUrl = getEnv("FAUCET_PUBLIC_URL")
	maxTokens, err = strconv.ParseFloat(getEnv("MAX_TOKENS_ALLOWED"), 64)
	if err != nil {
		log.Fatal("MAX_TOKENS_ALLOWED value is invalid")
	}

	recaptcha.Init(recaptchaSecretKey)

	http.HandleFunc("/claim", getCoinsHandler)

	if err := http.ListenAndServe(publicUrl, nil); err != nil {
		log.Fatal("failed to start server", err)
	}
}

func executeCmd(command string, writes ...string) {
	cmd, wc, _ := goExecute(command)

	for _, write := range writes {
		wc.Write([]byte(write + "\n"))
	}
	cmd.Wait()
}

func goExecute(command string) (cmd *exec.Cmd, pipeIn io.WriteCloser, pipeOut io.ReadCloser) {
	cmd = getCmd(command)
	pipeIn, _ = cmd.StdinPipe()
	pipeOut, _ = cmd.StdoutPipe()
	go cmd.Start()
	time.Sleep(time.Second)
	return cmd, pipeIn, pipeOut
}

func getCmd(command string) *exec.Cmd {
	// split command into command and args
	split := strings.Split(command, " ")

	var cmd *exec.Cmd
	if len(split) == 1 {
		cmd = exec.Command(split[0])
	} else {
		cmd = exec.Command(split[0], split[1:]...)
	}

	return cmd
}

func CheckAccountBalance(address string, amountFaucet string, key string) error {
	var queryRes AccountQueryRes
	var balance float64

	command := fmt.Sprintf("akashctl query account %s --node %v --chain-id %v -o json", address, node, chain)
	fmt.Println(" command ", command)

	out, accErr := exec.Command("bash", "-c", command).Output()

	if accErr == nil {
		if err := json.Unmarshal(out, &queryRes); err != nil {
			fmt.Printf("Error unmarshalling command line output %v", err)
			return err
		}
	}

	if len(queryRes.Value.Coins) == 0 {
		return nil
	}

	balance, err := strconv.ParseFloat(queryRes.Value.Coins[0].Amount, 64)
	if err != nil {
		return nil
	}

	if balance < maxTokens || accErr != nil {
		return nil
	}

	return errors.New("You have enough tokens in your account")
}

func getCoinsHandler(res http.ResponseWriter, request *http.Request) {
	address := request.FormValue("address")
	captchaResponse := request.FormValue("response")

	fmt.Println("No error", address, captchaResponse)

	(res).Header().Set("Access-Control-Allow-Origin", "*")

	// // make sure address is bech32
	// readableAddress, decodedAddress, decodeErr := bech32.DecodeAndConvert(claim.Address)
	// if decodeErr != nil {
	// 	panic(decodeErr)
	// }
	// // re-encode the address in bech32
	// encodedAddress, encodeErr := bech32.ConvertAndEncode(readableAddress, decodedAddress)
	// if encodeErr != nil {
	// 	panic(encodeErr)
	// }

	if len(address) != ADDR_LENGTH {
		panic("Invalid address")
	}

	// make sure captcha is valid
	clientIP := realip.FromRequest(request)
	captchaPassed, captchaErr := recaptcha.Confirm(clientIP, captchaResponse)
	if captchaErr != nil {
		panic(captchaErr)
	}

	fmt.Println("Captcha passed? ", captchaPassed)

	if !captchaPassed {
		res.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(res).Encode(ErrorResponse{
			Status:  false,
			Message: "Invalid captcha",
		})
		return
	}

	if captchaPassed {

		//check account balance
		err := CheckAccountBalance(address, amountFaucet, key)

		if err != nil {
			res.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(res).Encode(ErrorResponse{
				Status:  false,
				Message: err.Error(),
				Error:   err,
			})
			return
		}

		// send the coins!

		sendFaucet := fmt.Sprintf(
			"akashctl tx send %v %v %v --from %v --node %v --chain-id %v -y",
			key, address, amountFaucet, key, node, chain)
		fmt.Println(time.Now().UTC().Format(time.RFC3339), sendFaucet)

		executeCmd(sendFaucet, pass, pass)
	}

	res.WriteHeader(http.StatusOK)
	json.NewEncoder(res).Encode(SuccessResponse{
		Status: true,
		Data:   address,
	})

	return
}
